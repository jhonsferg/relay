package compress_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/ext/compress"
)

// BenchmarkZstdDictCompressor_CompressDecompress measures the raw
// encoder/decoder hot path (no HTTP involved) used on every compressed
// request/response body.
func BenchmarkZstdDictCompressor_CompressDecompress(b *testing.B) {
	c, err := compress.NewZstdDictionaryCompressor(nil)
	if err != nil {
		b.Fatalf("NewZstdDictionaryCompressor: %v", err)
	}
	payload := bytes.Repeat([]byte(`{"id":1,"name":"relay-benchmark-payload"}`), 50)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		compressed, err := c.Compress(payload)
		if err != nil {
			b.Fatalf("Compress: %v", err)
		}
		if _, err := c.Decompress(compressed); err != nil {
			b.Fatalf("Decompress: %v", err)
		}
	}
}

// BenchmarkWithZstdDictionary_RoundTrip measures the full transport
// middleware hot path: compressing the request body and transparently
// decompressing a zstd-encoded response through a relay.Client.
func BenchmarkWithZstdDictionary_RoundTrip(b *testing.B) {
	reqPayload := bytes.Repeat([]byte(`{"ping":"pong"}`), 20)

	enc, err := compress.NewZstdDictionaryCompressor(nil)
	if err != nil {
		b.Fatalf("NewZstdDictionaryCompressor: %v", err)
	}
	respBody := bytes.Repeat([]byte(`{"id":1,"name":"relay-benchmark-payload"}`), 50)
	compressedResp, err := enc.Compress(respBody)
	if err != nil {
		b.Fatalf("compress response fixture: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Drain and discard the (compressed) request body.
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Encoding", "zstd")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(compressedResp)
	}))
	defer srv.Close()

	opt, err := compress.WithZstdDictionary(nil)
	if err != nil {
		b.Fatalf("WithZstdDictionary: %v", err)
	}

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
		opt,
	)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Post("/").WithBody(reqPayload))
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if len(resp.Body()) != len(respBody) {
			b.Fatalf("body len = %d, want %d", len(resp.Body()), len(respBody))
		}
		relay.PutResponse(resp)
	}
}
