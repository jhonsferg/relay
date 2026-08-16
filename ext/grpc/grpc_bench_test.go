package grpc_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhonsferg/relay"
	relaygrpc "github.com/jhonsferg/relay/ext/grpc"
)

// BenchmarkWithMetadata_RoundTrip measures the per-request overhead of the
// WithOnBeforeRequest hooks installed by WithMetadata/WithBinaryMetadata/
// WithTimeoutHeader - the hot path exercised on every outgoing request.
func BenchmarkWithMetadata_RoundTrip(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relaygrpc.WithMetadata("x-request-id", "req-123"),
		relaygrpc.WithMetadata("x-tenant-id", "tenant-456"),
		relaygrpc.WithBinaryMetadata("token", []byte("binary-token-payload")),
		relaygrpc.WithTimeoutHeader(),
	)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Get("/"))
		if err != nil {
			b.Fatalf("Execute: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	}
}

// BenchmarkParseMetadata measures ParseMetadata's per-call cost, used to
// decode gRPC-Gateway metadata echoed back in response headers.
func BenchmarkParseMetadata(b *testing.B) {
	headers := map[string][]string{
		"Grpc-Metadata-X-Request-Id": {"req-123"},
		"Grpc-Metadata-X-Tenant-Id":  {"tenant-456"},
		"Grpc-Metadata-Token-Bin":    {"YmluYXJ5LXRva2VuLXBheWxvYWQ="},
		"Content-Type":               {"application/json"},
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := relaygrpc.ParseMetadata(headers); err != nil {
			b.Fatalf("ParseMetadata: %v", err)
		}
	}
}
