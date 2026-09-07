package jitterbug_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/ext/jitterbug"
)

// BenchmarkDecorrelatedJitter_NoRetry measures the steady-state overhead of
// the decorrelatedTransport's RoundTrip hot path (isRetryable check, body
// preparation) when every request succeeds on the first attempt - the common
// case in production traffic.
func BenchmarkDecorrelatedJitter_NoRetry(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newBenchClient(srv, jitterbug.WithDecorrelatedJitter(jitterbug.Config{
		MaxAttempts: 5,
		Base:        1 * time.Millisecond,
		Cap:         10 * time.Millisecond,
	}))

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		resp, err := c.Execute(c.Get("/"))
		if err != nil {
			b.Fatalf("Execute: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	}
}

// BenchmarkLinearBackoff_NoRetry measures the equivalent hot path for
// linearTransport on the common no-retry case.
func BenchmarkLinearBackoff_NoRetry(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newBenchClient(srv, jitterbug.WithLinearBackoff(jitterbug.Config{
		MaxAttempts: 5,
		Base:        1 * time.Millisecond,
		Cap:         10 * time.Millisecond,
	}, 0.2))

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		resp, err := c.Execute(c.Get("/"))
		if err != nil {
			b.Fatalf("Execute: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	}
}

func newBenchClient(srv *httptest.Server, opt relay.Option) *relay.Client {
	return relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(5*time.Second),
		opt,
	)
}
