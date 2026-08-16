package chaos_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
	chaos "github.com/jhonsferg/relay/ext/chaos"
)

// BenchmarkChaosMiddleware_PassThrough measures the overhead the chaos
// transport middleware adds to the common case: no fault triggers and the
// request passes through to the base transport unchanged.
func BenchmarkChaosMiddleware_PassThrough(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
		chaos.Middleware(chaos.Config{
			ErrorRate:   0,
			LatencyRate: 0,
			FaultRate:   0,
		}),
	)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Get("/"))
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		relay.PutResponse(resp)
	}
}

// BenchmarkChaosMiddleware_FaultInjected measures the short-circuit path
// where every request is answered with a synthetic fault status code
// without reaching the base transport.
func BenchmarkChaosMiddleware_FaultInjected(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
		relay.WithTimeout(5*time.Second),
		chaos.Middleware(chaos.Config{
			Faults:    []int{503},
			FaultRate: 1.0,
		}),
	)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Get("/"))
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		relay.PutResponse(resp)
	}
}
