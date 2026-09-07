package brotli_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	brotlienc "github.com/andybalholm/brotli"

	"github.com/jhonsferg/relay"
	relaybrotli "github.com/jhonsferg/relay/ext/brotli"
)

// BenchmarkBrotli_Decompress measures the per-request overhead of the brotli
// transport middleware decompressing a br-encoded response body end to end
// through a relay.Client.
func BenchmarkBrotli_Decompress(b *testing.B) {
	payload := bytes.Repeat([]byte(`{"id":1,"name":"relay-benchmark-payload"}`), 50)

	var compressed bytes.Buffer
	w := brotlienc.NewWriter(&compressed)
	if _, err := w.Write(payload); err != nil {
		b.Fatalf("compress payload: %v", err)
	}
	if err := w.Close(); err != nil {
		b.Fatalf("close writer: %v", err)
	}
	compressedBytes := compressed.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Encoding", "br")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write(compressedBytes)
	}))
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
		relaybrotli.WithBrotliDecompression(),
	)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Get("/"))
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if len(resp.Body()) != len(payload) {
			b.Fatalf("body len = %d, want %d", len(resp.Body()), len(payload))
		}
		relay.PutResponse(resp)
	}
}
