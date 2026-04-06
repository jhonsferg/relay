package relay_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	brotlienc "github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"

	"github.com/jhonsferg/relay"
)

// zstdCompress compresses data using zstd and returns the result.
func zstdCompress(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatalf("zstd.NewWriter: %v", err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatalf("zstd write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zstd close: %v", err)
	}
	return buf.Bytes()
}

// gzipCompress compresses data using gzip and returns the result.
func gzipCompress(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// brotliCompress compresses data using brotli and returns the result.
func brotliCompress(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := brotlienc.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatalf("brotli write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("brotli close: %v", err)
	}
	return buf.Bytes()
}

// newTestClient creates a relay client pointed at srv with retry and circuit
// breaker disabled, plus the supplied options.
func newTestClient(t *testing.T, srv *httptest.Server, opts ...relay.Option) *relay.Client {
	t.Helper()
	base := make([]relay.Option, 0, 3+len(opts))
	base = append(base,
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)
	return relay.New(append(base, opts...)...)
}

// ---- TestZstdResponseDecompression ----------------------------------------

func TestZstdResponseDecompression(t *testing.T) {
	t.Parallel()

	const want = "hello zstd world"
	compressed := zstdCompress(t, []byte(want))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "zstd")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write(compressed) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, relay.WithCompression(relay.CompressionZstd))

	resp, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// ---- TestZstdAcceptEncoding ------------------------------------------------

func TestZstdAcceptEncoding(t *testing.T) {
	t.Parallel()

	var capturedEncoding string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedEncoding = r.Header.Get("Accept-Encoding")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, relay.WithCompression(relay.CompressionAuto))

	if _, err := c.Execute(c.Get("/")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, tok := range []string{"zstd", "br", "gzip", "deflate"} {
		if !strings.Contains(capturedEncoding, tok) {
			t.Errorf("Accept-Encoding %q does not contain %q", capturedEncoding, tok)
		}
	}
}

// ---- TestRequestBodyCompression -------------------------------------------

func TestRequestBodyCompression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		algo           relay.CompressionAlgorithm
		body           string
		minBytes       int
		wantEncoding   string
		wantCompressed bool
	}{
		{
			name:           "zstd compresses above threshold",
			algo:           relay.CompressionZstd,
			body:           strings.Repeat("a", 2048),
			minBytes:       512,
			wantEncoding:   "zstd",
			wantCompressed: true,
		},
		{
			name:           "auto compresses with zstd above threshold",
			algo:           relay.CompressionAuto,
			body:           strings.Repeat("b", 2048),
			minBytes:       512,
			wantEncoding:   "zstd",
			wantCompressed: true,
		},
		{
			name:           "gzip compresses above threshold",
			algo:           relay.CompressionGzip,
			body:           strings.Repeat("c", 2048),
			minBytes:       512,
			wantEncoding:   "gzip",
			wantCompressed: true,
		},
		{
			name:           "brotli compresses above threshold",
			algo:           relay.CompressionBrotli,
			body:           strings.Repeat("d", 2048),
			minBytes:       512,
			wantEncoding:   "br",
			wantCompressed: true,
		},
		{
			name:           "body below threshold is not compressed",
			algo:           relay.CompressionZstd,
			body:           "small",
			minBytes:       1024,
			wantEncoding:   "",
			wantCompressed: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var (
				gotEncoding string
				gotBody     []byte
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotEncoding = r.Header.Get("Content-Encoding")
				gotBody, _ = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(srv.Close)

			c := newTestClient(t, srv, relay.WithRequestCompression(tc.algo, tc.minBytes))

			req := c.Post("/").WithBody([]byte(tc.body))
			if _, err := c.Execute(req); err != nil {
				t.Fatalf("Execute: %v", err)
			}

			if gotEncoding != tc.wantEncoding {
				t.Errorf("Content-Encoding = %q, want %q", gotEncoding, tc.wantEncoding)
			}
			if tc.wantCompressed {
				if bytes.Equal(gotBody, []byte(tc.body)) {
					t.Error("body was not compressed (identical to original)")
				}
			} else {
				if !bytes.Equal(gotBody, []byte(tc.body)) {
					t.Error("body should not be compressed (body differs from original)")
				}
			}
		})
	}
}

// ---- Table-driven algorithm tests -----------------------------------------

func TestCompressionAlgorithms_ResponseDecompression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		encoding string
		compress func([]byte) []byte
		algo     relay.CompressionAlgorithm
	}{
		{
			name:     "zstd",
			encoding: "zstd",
			compress: func(b []byte) []byte { return zstdCompress(t, b) },
			algo:     relay.CompressionZstd,
		},
		{
			name:     "gzip",
			encoding: "gzip",
			compress: func(b []byte) []byte { return gzipCompress(t, b) },
			algo:     relay.CompressionGzip,
		},
		{
			name:     "brotli",
			encoding: "br",
			compress: func(b []byte) []byte { return brotliCompress(t, b) },
			algo:     relay.CompressionBrotli,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			const want = "compressed response body"
			compressed := tc.compress([]byte(want))

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Encoding", tc.encoding)
				w.WriteHeader(http.StatusOK)
				w.Write(compressed) //nolint:errcheck
			}))
			t.Cleanup(srv.Close)

			c := newTestClient(t, srv, relay.WithCompression(tc.algo))

			resp, err := c.Execute(c.Get("/"))
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if got := resp.String(); got != want {
				t.Errorf("body = %q, want %q", got, want)
			}
			// Content-Encoding should have been stripped by the transport.
			if enc := resp.Header("Content-Encoding"); enc != "" {
				t.Errorf("Content-Encoding = %q after decompression, want empty", enc)
			}
		})
	}
}

func TestCompressionAlgorithms_AcceptEncoding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		algo        relay.CompressionAlgorithm
		wantContain []string
	}{
		{
			name:        "auto sends all",
			algo:        relay.CompressionAuto,
			wantContain: []string{"zstd", "br", "gzip", "deflate"},
		},
		{
			name:        "zstd only",
			algo:        relay.CompressionZstd,
			wantContain: []string{"zstd"},
		},
		{
			name:        "gzip only",
			algo:        relay.CompressionGzip,
			wantContain: []string{"gzip"},
		},
		{
			name:        "brotli only",
			algo:        relay.CompressionBrotli,
			wantContain: []string{"br"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var capturedEncoding string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedEncoding = r.Header.Get("Accept-Encoding")
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(srv.Close)

			c := newTestClient(t, srv, relay.WithCompression(tc.algo))
			if _, err := c.Execute(c.Get("/")); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			for _, tok := range tc.wantContain {
				if !strings.Contains(capturedEncoding, tok) {
					t.Errorf("[%s] Accept-Encoding %q does not contain %q", tc.name, capturedEncoding, tok)
				}
			}
		})
	}
}

// TestAutoDecompressZstd verifies that CompressionAuto decompresses a zstd response.
func TestAutoDecompressZstd(t *testing.T) {
	t.Parallel()

	const want = "auto zstd body"
	compressed := zstdCompress(t, []byte(want))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "zstd")
		w.WriteHeader(http.StatusOK)
		w.Write(compressed) //nolint:errcheck
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, relay.WithCompression(relay.CompressionAuto))

	resp, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := resp.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// TestRequestCompressionDefaultThreshold verifies the default 1024-byte threshold.
func TestRequestCompressionDefaultThreshold(t *testing.T) {
	t.Parallel()

	var gotEncoding string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEncoding = r.Header.Get("Content-Encoding")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// Pass minBytes=0 to use the default (1024).
	c := newTestClient(t, srv, relay.WithRequestCompression(relay.CompressionZstd, 0))

	smallBody := strings.Repeat("x", 512) // below default 1024
	req := c.Post("/").WithBody([]byte(smallBody))
	if _, err := c.Execute(req); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotEncoding != "" {
		t.Errorf("Content-Encoding = %q, want empty for body below threshold", gotEncoding)
	}

	largeBody := strings.Repeat("x", 2048) // above default 1024
	req = c.Post("/").WithBody([]byte(largeBody))
	if _, err := c.Execute(req); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotEncoding != "zstd" {
		t.Errorf("Content-Encoding = %q, want zstd for body above threshold", gotEncoding)
	}
}
