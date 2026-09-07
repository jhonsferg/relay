package slog

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhonsferg/relay"
)

// BenchmarkRequestResponseLogging measures the per-request overhead of the
// OnAfterResponse logging hook (logResponse), which runs on every successful
// response, end to end through a relay.Client. The handler discards output
// so the benchmark isolates the middleware's own cost rather than I/O.
func BenchmarkRequestResponseLogging(b *testing.B) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := relay.New(
		relay.WithBaseURL(server.URL),
		WithRequestResponseLogging(logger),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Get("/bench"))
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("unexpected status %d", resp.StatusCode)
		}
		relay.PutResponse(resp)
	}
}

// BenchmarkRequestResponseLogging_Retry measures the BeforeRetryHooks path
// (logRetry), which runs on every retried attempt, in addition to the
// OnAfterResponse path exercised above.
func BenchmarkRequestResponseLogging_Retry(b *testing.B) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls%2 == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := relay.New(
		relay.WithBaseURL(server.URL),
		WithRequestResponseLogging(logger),
		relay.WithRetry(&relay.RetryConfig{
			MaxAttempts:     2,
			InitialInterval: 1,
			MaxInterval:     1,
			Multiplier:      1,
			RetryableStatus: []int{http.StatusServiceUnavailable},
		}),
		relay.WithDisableCircuitBreaker(),
	)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Get("/bench"))
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("unexpected status %d", resp.StatusCode)
		}
		relay.PutResponse(resp)
	}
}
