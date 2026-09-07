package gobreaker_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	gb "github.com/sony/gobreaker"

	"github.com/jhonsferg/relay"
	relaybreaker "github.com/jhonsferg/relay/ext/breaker/gobreaker"
)

// BenchmarkGoBreaker_Closed measures the per-request overhead of routing a
// successful request through the gobreaker transport middleware while the
// breaker stays in the Closed state (the common case).
func BenchmarkGoBreaker_Closed(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cb := relaybreaker.NewCircuitBreaker(gb.Settings{
		Name:        "bench",
		MaxRequests: 1,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gb.Counts) bool {
			return counts.ConsecutiveFailures >= 1000000
		},
	})

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
		relaybreaker.WithGoBreaker(cb),
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

// BenchmarkGoBreaker_Open measures the overhead of the short-circuit path
// when the breaker is Open and requests are rejected without reaching the
// base transport.
func BenchmarkGoBreaker_Open(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cb := relaybreaker.NewCircuitBreaker(gb.Settings{
		Name:        "bench-open",
		MaxRequests: 1,
		Timeout:     time.Hour,
		ReadyToTrip: func(counts gb.Counts) bool {
			return counts.ConsecutiveFailures >= 1
		},
	})

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
		relaybreaker.WithGoBreaker(cb),
	)

	// Trip the breaker once.
	resp, _ := client.Execute(client.Get("/"))
	if resp != nil {
		relay.PutResponse(resp)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = client.Execute(client.Get("/"))
	}
}
