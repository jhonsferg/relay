// Package brotli adds transparent Brotli (br) decompression to the relay HTTP
// client. When enabled, the client advertises "br" in Accept-Encoding and
// automatically decompresses responses with Content-Encoding: br.
//
// Usage:
//
//	import (
//	    "github.com/jhonsferg/relay"
//	    relaybrotli "github.com/jhonsferg/relay/ext/brotli"
//	)
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relaybrotli.WithBrotliDecompression(),
//	)
package brotli

import (
	"io"
	"net/http"
	"strings"

	"github.com/andybalholm/brotli"

	"github.com/jhonsferg/relay"
)

// WithBrotliDecompression returns a [relay.Option] that enables transparent
// Brotli decompression of HTTP responses. It adds "br" to Accept-Encoding and
// decompresses responses with Content-Encoding: br automatically.
func WithBrotliDecompression() relay.Option {
	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &brotliTransport{base: next}
	})
}

// brotliTransport wraps an http.RoundTripper and transparently decompresses
// responses with Content-Encoding: br.
type brotliTransport struct {
	base http.RoundTripper
}

// RoundTrip advertises brotli support and decompresses br-encoded responses.
func (t *brotliTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request so we can safely mutate headers.
	r := req.Clone(req.Context())

	existing := r.Header.Get("Accept-Encoding")
	if existing == "" {
		r.Header.Set("Accept-Encoding", "br, gzip, deflate")
	} else if !strings.Contains(existing, "br") {
		r.Header.Set("Accept-Encoding", "br, "+existing)
	}

	resp, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}

	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "br") {
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
		resp.ContentLength = -1

		original := resp.Body
		br := brotli.NewReader(original)
		resp.Body = &readCloser{reader: br, closer: original}
	}

	return resp, nil
}

// readCloser combines a brotli reader with the original closer.
type readCloser struct {
	reader io.Reader
	closer io.Closer
}

func (b *readCloser) Read(p []byte) (int, error) { return b.reader.Read(p) }
func (b *readCloser) Close() error               { return b.closer.Close() }
