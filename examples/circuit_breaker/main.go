// Package main demonstrates relay's circuit breaker: configuring
// CircuitBreakerConfig with all fields, observing state transitions via
// OnStateChange, inspecting IsHealthy() / CircuitBreakerState(), and manually
// resetting with ResetCircuitBreaker().
package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	relay "github.com/jhonsferg/relay"
)

func main() {
	// ---------------------------------------------------------------------------
	// Test server: fails for the first N requests, then returns 200.
	// ---------------------------------------------------------------------------
	var failRemaining atomic.Int32
	failRemaining.Store(6) // enough to trip the circuit breaker

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failRemaining.Add(-1) >= 0 {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintln(w, `{"error":"simulated failure"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	}))
	defer srv.Close()

	// ---------------------------------------------------------------------------
	// CircuitBreakerConfig — every field documented.
	// ---------------------------------------------------------------------------
	cbCfg := &relay.CircuitBreakerConfig{
		// Trip to Open after this many consecutive failures in the Closed state.
		MaxFailures: 3,

		// Stay Open for this long before probing with a HalfOpen attempt.
		// Kept short here so the example completes quickly.
		ResetTimeout: 1 * time.Second,

		// While HalfOpen, allow at most this many probe requests.
		HalfOpenRequests: 2,

		// Require this many consecutive successes while HalfOpen to close again.
		SuccessThreshold: 2,

		// OnStateChange is called on every transition. The callback runs with
		// the circuit breaker's internal mutex held — do NOT call back into the
		// client from inside this callback.
		OnStateChange: func(from, to relay.CircuitBreakerState) {
			fmt.Printf("[circuit breaker] %s → %s\n", from, to)
		},
	}

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithCircuitBreaker(cbCfg),
		// Disable retries so each failure counts immediately toward the breaker.
		relay.WithDisableRetry(),
	)

	// ---------------------------------------------------------------------------
	// Phase 1: send requests until the circuit trips.
	// ---------------------------------------------------------------------------
	fmt.Println("=== Phase 1: tripping the circuit breaker ===")
	for i := 1; i <= 5; i++ {
		resp, err := client.Execute(client.Get("/api"))
		state := client.CircuitBreakerState()
		healthy := client.IsHealthy()

		if errors.Is(err, relay.ErrCircuitOpen) {
			fmt.Printf("  request %d: circuit is OPEN — rejected without network call\n", i)
			fmt.Printf("  IsHealthy()=%v  State=%s\n", healthy, state)
			continue
		}
		if err != nil {
			fmt.Printf("  request %d: error=%v  state=%s  healthy=%v\n", i, err, state, healthy)
			continue
		}
		fmt.Printf("  request %d: status=%d  state=%s  healthy=%v\n", i, resp.StatusCode, state, healthy)
	}

	// ---------------------------------------------------------------------------
	// Phase 2: inspect state while Open.
	// ---------------------------------------------------------------------------
	fmt.Println("\n=== Phase 2: state while Open ===")
	fmt.Printf("  CircuitBreakerState() = %s\n", client.CircuitBreakerState())
	fmt.Printf("  IsHealthy()           = %v\n", client.IsHealthy())

	// ---------------------------------------------------------------------------
	// Phase 3: wait for ResetTimeout, then probe (HalfOpen).
	// ---------------------------------------------------------------------------
	fmt.Printf("\n=== Phase 3: waiting %s for reset timeout ===\n", cbCfg.ResetTimeout)
	time.Sleep(cbCfg.ResetTimeout + 100*time.Millisecond)

	// The server now returns 200 (failRemaining is exhausted).
	failRemaining.Store(-100)

	fmt.Println("  sending probe requests while HalfOpen…")
	for i := 1; i <= int(cbCfg.SuccessThreshold); i++ {
		resp, err := client.Execute(client.Get("/api"))
		if err != nil {
			fmt.Printf("  probe %d: err=%v  state=%s\n", i, err, client.CircuitBreakerState())
			continue
		}
		fmt.Printf("  probe %d: status=%d  state=%s  healthy=%v\n",
			i, resp.StatusCode, client.CircuitBreakerState(), client.IsHealthy())
	}

	// ---------------------------------------------------------------------------
	// Phase 4: circuit is Closed again — normal operation resumes.
	// ---------------------------------------------------------------------------
	fmt.Println("\n=== Phase 4: circuit closed — normal operation ===")
	resp, err := client.Execute(client.Get("/api"))
	if err != nil {
		log.Fatalf("unexpected error after recovery: %v", err)
	}
	fmt.Printf("  status=%d  state=%s  healthy=%v\n",
		resp.StatusCode, client.CircuitBreakerState(), client.IsHealthy())

	// ---------------------------------------------------------------------------
	// Phase 5: manual reset.
	//
	// ResetCircuitBreaker() forces the breaker back to Closed and clears all
	// counters. Use this after a manual health check confirms recovery.
	// ---------------------------------------------------------------------------
	fmt.Println("\n=== Phase 5: manual ResetCircuitBreaker ===")
	// Artificially re-open it by sending failing requests.
	failRemaining.Store(10)
	for i := 0; i < cbCfg.MaxFailures; i++ {
		_, _ = client.Execute(client.Get("/api")) //nolint:errcheck
	}
	fmt.Printf("  state before reset: %s\n", client.CircuitBreakerState())

	client.ResetCircuitBreaker()
	fmt.Printf("  state after  reset: %s\n", client.CircuitBreakerState())
	fmt.Printf("  IsHealthy()       : %v\n", client.IsHealthy())
}
