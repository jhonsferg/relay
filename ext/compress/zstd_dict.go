// Package compress provides zstd compression helpers for the relay HTTP client,
// including support for pre-trained dictionaries that improve compression ratios
// for small, structurally similar payloads.
//
// Usage:
//
//	opt, err := compress.WithZstdDictionary(dict)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	client := relay.New(relay.WithBaseURL("https://api.example.com"), opt)
package compress

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/klauspost/compress/zstd"

	"github.com/jhonsferg/relay"
)

// ZstdDictCompressor is a zstd compressor/decompressor that uses a pre-trained
// dictionary. Dictionaries dramatically improve compression ratio for small,
// similar payloads. The encoder and decoder are created once and are safe for
// concurrent use.
type ZstdDictCompressor struct {
	encoder *zstd.Encoder
	decoder *zstd.Decoder
}

// NewZstdDictionaryCompressor creates a new compressor using the given
// pre-trained zstd dictionary. Pass nil (or empty) dict to fall back to
// standard zstd without a dictionary.
func NewZstdDictionaryCompressor(dict []byte) (*ZstdDictCompressor, error) {
	var (
		enc *zstd.Encoder
		dec *zstd.Decoder
		err error
	)

	if len(dict) > 0 {
		enc, err = zstd.NewWriter(nil, zstd.WithEncoderDict(dict))
		if err != nil {
			return nil, fmt.Errorf("zstd: create encoder with dict: %w", err)
		}
		dec, err = zstd.NewReader(nil, zstd.WithDecoderDicts(dict))
		if err != nil {
			enc.Close() //nolint:errcheck
			return nil, fmt.Errorf("zstd: create decoder with dict: %w", err)
		}
	} else {
		enc, err = zstd.NewWriter(nil)
		if err != nil {
			return nil, fmt.Errorf("zstd: create encoder: %w", err)
		}
		dec, err = zstd.NewReader(nil)
		if err != nil {
			enc.Close() //nolint:errcheck
			return nil, fmt.Errorf("zstd: create decoder: %w", err)
		}
	}

	return &ZstdDictCompressor{encoder: enc, decoder: dec}, nil
}

// Compress compresses data using the pre-trained dictionary (if any).
func (z *ZstdDictCompressor) Compress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	z.encoder.Reset(&buf)
	if _, err := z.encoder.Write(data); err != nil {
		return nil, fmt.Errorf("zstd: compress write: %w", err)
	}
	if err := z.encoder.Close(); err != nil {
		return nil, fmt.Errorf("zstd: compress close: %w", err)
	}
	return buf.Bytes(), nil
}

// Decompress decompresses data that was compressed with the matching
// dictionary.
func (z *ZstdDictCompressor) Decompress(data []byte) ([]byte, error) {
	if err := z.decoder.Reset(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("zstd: decompress reset: %w", err)
	}
	out, err := io.ReadAll(z.decoder)
	if err != nil {
		return nil, fmt.Errorf("zstd: decompress read: %w", err)
	}
	return out, nil
}

// Encoding returns "zstd" to satisfy any Compressor interface.
func (z *ZstdDictCompressor) Encoding() string { return "zstd" }

// WithZstdDictionary returns a relay middleware [relay.Option] that uses a
// pre-trained zstd dictionary for request body compression and transparent
// response decompression. Passing nil (or an empty slice) falls back to
// standard zstd without a dictionary.
func WithZstdDictionary(dict []byte) (relay.Option, error) {
	c, err := NewZstdDictionaryCompressor(dict)
	if err != nil {
		return nil, err
	}
	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &zstdDictTransport{base: next, comp: c}
	}), nil
}

// zstdDictTransport wraps an http.RoundTripper and transparently
// compresses request bodies / decompresses zstd response bodies.
type zstdDictTransport struct {
	base http.RoundTripper
	comp *ZstdDictCompressor
}

// RoundTrip compresses the request body (if present) and decompresses
// zstd-encoded responses.
func (t *zstdDictTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())

	// Compress the request body when present.
	if r.Body != nil && r.Body != http.NoBody {
		raw, err := io.ReadAll(r.Body)
		r.Body.Close() //nolint:errcheck
		if err != nil {
			return nil, fmt.Errorf("zstd transport: read request body: %w", err)
		}
		compressed, err := t.comp.Compress(raw)
		if err != nil {
			return nil, fmt.Errorf("zstd transport: compress request: %w", err)
		}
		r.Body = io.NopCloser(bytes.NewReader(compressed))
		r.ContentLength = int64(len(compressed))
		r.Header.Set("Content-Encoding", "zstd")
	}

	// Advertise zstd support.
	existing := r.Header.Get("Accept-Encoding")
	if existing == "" {
		r.Header.Set("Accept-Encoding", "zstd, gzip, deflate")
	} else if !strings.Contains(existing, "zstd") {
		r.Header.Set("Accept-Encoding", "zstd, "+existing)
	}

	resp, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}

	// Decompress response body when the server sent zstd.
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "zstd") {
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
		resp.ContentLength = -1

		original := resp.Body
		resp.Body = &zstdReadCloser{comp: t.comp, src: original}
	}

	return resp, nil
}

// zstdReadCloser decompresses a zstd-encoded response body on the fly.
// init is used to decompress exactly once even when Read is called from
// multiple goroutines (e.g. concurrent reads during context cancellation).
// closed is an atomic flag so that Close is idempotent and safe to call
// concurrently with the last Read.
type zstdReadCloser struct {
	comp   *ZstdDictCompressor
	src    io.ReadCloser
	buf    *bytes.Reader
	once   sync.Once
	initErr error
	closed  atomic.Bool
}

func (z *zstdReadCloser) Read(p []byte) (int, error) {
	z.once.Do(func() {
		raw, err := io.ReadAll(z.src)
		if err != nil {
			z.initErr = err
			return
		}
		dec, err := z.comp.Decompress(raw)
		if err != nil {
			z.initErr = err
			return
		}
		z.buf = bytes.NewReader(dec)
	})
	if z.initErr != nil {
		return 0, z.initErr
	}
	return z.buf.Read(p)
}

func (z *zstdReadCloser) Close() error {
	if z.closed.Swap(true) {
		return nil
	}
	return z.src.Close()
}
