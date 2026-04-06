package relay

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// CompressionAlgorithm specifies the compression algorithm used for Accept-Encoding
// negotiation and response/request body compression.
type CompressionAlgorithm int

const (
	// CompressionAuto advertises all supported encodings (zstd, br, gzip, deflate)
	// and decompresses responses automatically. This is the recommended default.
	CompressionAuto CompressionAlgorithm = iota

	// CompressionZstd selects Zstandard (zstd) compression only.
	CompressionZstd

	// CompressionBrotli selects Brotli (br) compression only.
	CompressionBrotli

	// CompressionGzip selects gzip compression only.
	CompressionGzip
)

// acceptEncoding returns the Accept-Encoding header value for the algorithm.
func (a CompressionAlgorithm) acceptEncoding() string {
	switch a {
	case CompressionZstd:
		return "zstd"
	case CompressionBrotli:
		return "br"
	case CompressionGzip:
		return "gzip"
	default: // CompressionAuto
		return "zstd, br, gzip, deflate"
	}
}

// WithCompression returns an [Option] that enables transparent response
// decompression. The client advertises the chosen algorithm(s) via
// Accept-Encoding and automatically decompresses responses whose
// Content-Encoding matches.
//
// When [CompressionAuto] is used the header sent is:
//
//	Accept-Encoding: zstd, br, gzip, deflate
//
// Existing [WithDisableCompression] or transport-level compression settings
// are unaffected; this middleware operates at the transport layer.
func WithCompression(algo CompressionAlgorithm) Option {
	return WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &compressionTransport{base: next, algo: algo}
	})
}

// compressionTransport wraps an http.RoundTripper and transparently
// decompresses responses for the configured algorithm(s).
type compressionTransport struct {
	base http.RoundTripper
	algo CompressionAlgorithm
}

// RoundTrip injects Accept-Encoding and decompresses the response body.
func (t *compressionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("Accept-Encoding", t.algo.acceptEncoding())

	resp, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}

	encoding := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	switch encoding {
	case "zstd":
		dec, decErr := zstd.NewReader(resp.Body)
		if decErr != nil {
			resp.Body.Close() //nolint:errcheck
			return nil, fmt.Errorf("relay: zstd decoder: %w", decErr)
		}
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
		resp.ContentLength = -1
		resp.Body = &zstdReadCloser{decoder: dec, closer: resp.Body}

	case "br":
		br := brotli.NewReader(resp.Body)
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
		resp.ContentLength = -1
		resp.Body = &genericReadCloser{reader: br, closer: resp.Body}

	case "gzip":
		gr, grErr := gzip.NewReader(resp.Body)
		if grErr != nil {
			resp.Body.Close() //nolint:errcheck
			return nil, fmt.Errorf("relay: gzip decoder: %w", grErr)
		}
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
		resp.ContentLength = -1
		resp.Body = &genericReadCloser{reader: gr, closer: resp.Body}

	case "deflate":
		fr := flate.NewReader(resp.Body)
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
		resp.ContentLength = -1
		resp.Body = &genericReadCloser{reader: fr, closer: resp.Body}
	}

	return resp, nil
}

// WithRequestCompression returns an [Option] that compresses outgoing request
// bodies when their serialised size exceeds minBytes (pass ≤ 0 to use the
// default of 1024 bytes). The Content-Encoding header is set accordingly.
//
// [CompressionAuto] compresses with zstd. For Brotli or Gzip pass the
// corresponding constant explicitly.
//
// Example:
//
//	client := relay.New(
//	    relay.WithRequestCompression(relay.CompressionZstd, 512),
//	)
func WithRequestCompression(algo CompressionAlgorithm, minBytes int) Option {
	if minBytes <= 0 {
		minBytes = 1024
	}
	return WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &requestCompressionTransport{base: next, algo: algo, minBytes: minBytes}
	})
}

// requestCompressionTransport compresses outgoing request bodies above a size
// threshold before handing off to the next RoundTripper.
type requestCompressionTransport struct {
	base     http.RoundTripper
	algo     CompressionAlgorithm
	minBytes int
}

// RoundTrip reads the request body, compresses it when above the threshold,
// then forwards the request with an updated Content-Encoding header.
func (t *requestCompressionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body == nil || req.ContentLength == 0 {
		return t.base.RoundTrip(req)
	}

	body, err := io.ReadAll(req.Body)
	req.Body.Close() //nolint:errcheck
	if err != nil {
		return nil, fmt.Errorf("relay: reading request body for compression: %w", err)
	}

	if len(body) < t.minBytes {
		r := req.Clone(req.Context())
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		return t.base.RoundTrip(r)
	}

	compressed, enc, compErr := compressBytes(t.algo, body)
	if compErr != nil {
		return nil, fmt.Errorf("relay: compressing request body: %w", compErr)
	}

	r := req.Clone(req.Context())
	r.Body = io.NopCloser(bytes.NewReader(compressed))
	r.ContentLength = int64(len(compressed))
	r.Header.Set("Content-Encoding", enc)
	return t.base.RoundTrip(r)
}

// compressBytes compresses body with the specified algorithm and returns the
// compressed bytes and the corresponding Content-Encoding token.
func compressBytes(algo CompressionAlgorithm, body []byte) ([]byte, string, error) {
	var buf bytes.Buffer

	switch algo {
	case CompressionBrotli:
		w := brotli.NewWriter(&buf)
		if _, err := w.Write(body); err != nil {
			return nil, "", err
		}
		if err := w.Close(); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "br", nil

	case CompressionGzip:
		w := gzip.NewWriter(&buf)
		if _, err := w.Write(body); err != nil {
			return nil, "", err
		}
		if err := w.Close(); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "gzip", nil

	default: // CompressionZstd and CompressionAuto both use zstd for requests
		enc, err := zstd.NewWriter(&buf)
		if err != nil {
			return nil, "", err
		}
		if _, err := enc.Write(body); err != nil {
			return nil, "", err
		}
		if err := enc.Close(); err != nil {
			return nil, "", err
		}
		return buf.Bytes(), "zstd", nil
	}
}

// zstdReadCloser wraps a zstd.Decoder and the original body closer.
type zstdReadCloser struct {
	decoder *zstd.Decoder
	closer  io.Closer
}

func (z *zstdReadCloser) Read(p []byte) (int, error) { return z.decoder.Read(p) }
func (z *zstdReadCloser) Close() error {
	z.decoder.Close()
	return z.closer.Close()
}

// genericReadCloser combines an arbitrary io.Reader with the original closer.
type genericReadCloser struct {
	reader io.Reader
	closer io.Closer
}

func (c *genericReadCloser) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *genericReadCloser) Close() error               { return c.closer.Close() }
