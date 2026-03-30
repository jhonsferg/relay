// Package main demonstrates relay's WithHealthCheck option, which starts a
// background goroutine that probes a health endpoint while the circuit breaker
// is open and resets it automatically once the upstream recovers — without
// waiting for the full ResetTimeout to elapse.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	relay "github.com/jhonsferg/relay"
)

func main() {
	// -------------------------------------------------------------------------
	// 1. Upstream server — starts unhealthy, recovers after 400 ms.
	// -------------------------------------------------------------------------
	var healthy atomic.Bool

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if healthy.Load() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer upstream.Close()

	// -------------------------------------------------------------------------
	// 2. Dedicated health endpoint (separate from the main API).
	//
	// In production this would be a lightweight /healthz or /ping route that
	// only checks internal readiness without touching shared state. Here we
	// reuse the same server for simplicity.
	// -------------------------------------------------------------------------
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if healthy.Load() {
			fmt.Fprint(w, `{"status":"ok"}`)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"status":"degraded"}`)
		}
	}))
	defer health.Close()

	// -------------------------------------------------------------------------
	// 3. Build a relay client with:
	//    - Aggressive circuit breaker (trips after 2 failures, 10-min reset).
	//    - Health check probe every 100 ms — beats the 10-min reset timeout.
	// -------------------------------------------------------------------------
	client := relay.New(
		relay.WithBaseURL(upstream.URL),
		relay.WithDisableRetry(), // one attempt per call for this demo
		relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
			MaxFailures:      2,
			ResetTimeout:     10 * time.Minute, // would take forever without health check
			HalfOpenRequests: 1,
			SuccessThreshold: 1,
			OnStateChange: func(from, to relay.CircuitBreakerState) {
				fmt.Printf("  circuit: %s → %s\n", from, to)
			},
		}),
		// Health check fires every 100 ms with a 1 s per-probe timeout.
		relay.WithHealthCheck(health.URL, 100*time.Millisecond, time.Second, http.StatusOK),
	)

	// -------------------------------------------------------------------------
	// 4. Trip the circuit breaker by sending requests while upstream is down.
	// -------------------------------------------------------------------------
	fmt.Println("=== Phase 1: upstream is down ===")
	for i := 1; i <= 3; i++ {
		resp, err := client.Execute(client.Get("/api"))
		if err != nil {
			fmt.Printf("  request %d: error=%v (circuit=%s)\n", i, err, client.CircuitBreakerState())
		} else {
			fmt.Printf("  request %d: status=%d (circuit=%s)\n", i, resp.StatusCode, client.CircuitBreakerState())
		}
	}

	// -------------------------------------------------------------------------
	// 5. Upstream recovers — health check resets the circuit automatically.
	// -------------------------------------------------------------------------
	fmt.Println("\n=== Phase 2: upstream recovers ===")
	healthy.Store(true)
	fmt.Println("  upstream is now healthy — waiting for health check probe…")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && client.CircuitBreakerState() != relay.StateClosed {
		time.Sleep(20 * time.Millisecond)
	}

	if client.CircuitBreakerState() != relay.StateClosed {
		log.Fatal("circuit was not reset within 2 s")
	}
	fmt.Printf("  circuit reset to %s after health check succeeded\n", client.CircuitBreakerState())

	// -------------------------------------------------------------------------
	// 6. Traffic flows again without any manual intervention.
	// -------------------------------------------------------------------------
	fmt.Println("\n=== Phase 3: traffic resumes ===")
	for i := 1; i <= 3; i++ {
		resp, err := client.Execute(client.Get("/api"))
		if err != nil {
			fmt.Printf("  request %d: error=%v\n", i, err)
		} else {
			fmt.Printf("  request %d: status=%d ✓\n", i, resp.StatusCode)
		}
	}

	// Graceful shutdown stops the health check goroutine.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	fmt.Println("\nclient shut down cleanly.")
}
