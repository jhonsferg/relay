// Package main demonstrates relay's retry subsystem: configuring every field
// of RetryConfig, structured logging via OnRetry, a custom RetryIf predicate,
// and the WithDisableRetry escape hatch for one-shot requests.
package main

import (
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	relay "github.com/jhonsferg/relay"
)

func main() {
	// ---------------------------------------------------------------------------
	// Test server that fails the first two requests with 503, then succeeds.
	// ---------------------------------------------------------------------------
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"error":"service unavailable","attempt":%d}`, n)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"result":"ok","attempt":%d}`, n)
	}))
	defer srv.Close()

	// ---------------------------------------------------------------------------
	// 1. Fully configured RetryConfig
	//
	// Every field is set explicitly so the example is self-documenting.
	// ---------------------------------------------------------------------------
	retryCfg := &relay.RetryConfig{
		// Total tries, including the initial attempt.
		MaxAttempts: 4,

		// Wait 200 ms before the first retry.
		InitialInterval: 200 * time.Millisecond,

		// Never wait longer than 5 s between retries.
		MaxInterval: 5 * time.Second,

		// Double the interval on each attempt.
		Multiplier: 2.0,

		// Add ±30 % random jitter to spread out thundering herds.
		RandomFactor: 0.3,

		// Retry these specific HTTP status codes in addition to network errors.
		RetryableStatus: []int{
			http.StatusTooManyRequests,      // 429
			http.StatusInternalServerError,  // 500
			http.StatusBadGateway,           // 502
			http.StatusServiceUnavailable,   // 503
			http.StatusGatewayTimeout,       // 504
		},

		// RetryIf is an optional veto: return false to skip a retry even when
		// the status or error is in the retryable set.
		// Here we never retry if the context was explicitly cancelled.
		RetryIf: func(resp *http.Response, err error) bool {
			if err != nil {
				// Skip retrying on context cancellation — that was intentional.
				return !errors.Is(err, http.ErrServerClosed)
			}
			// Only retry 503 when the server sends a Retry-After header.
			if resp != nil && resp.StatusCode == http.StatusServiceUnavailable {
				return true // always retry 503 in this example
			}
			return true
		},

		// OnRetry is called before each retry sleep, ideal for structured logs.
		// attempt is 1-based: the first retry is attempt 1.
		OnRetry: func(attempt int, resp *http.Response, err error) {
			logger := relay.NewDefaultLogger(slog.LevelDebug)
			if err != nil {
				logger.Warn("retrying after error",
					"attempt", attempt,
					"error", err.Error(),
				)
				return
			}
			if resp != nil {
				logger.Warn("retrying after non-2xx response",
					"attempt", attempt,
					"status", resp.StatusCode,
				)
			}
		},
	}

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithRetry(retryCfg),
		relay.WithTimeout(30*time.Second),
	)

	fmt.Println("=== Example 1: RetryConfig with all fields ===")
	resp, err := client.Execute(client.Get("/data"))
	if err != nil {
		log.Fatalf("request failed after all retries: %v", err)
	}
	fmt.Printf("Final status: %d — body: %s\n", resp.StatusCode, resp.String())
	fmt.Printf("Total server hits: %d\n\n", requestCount.Load())

	// ---------------------------------------------------------------------------
	// 2. WithDisableRetry — make a single-shot request without any retry logic.
	//
	// Useful for non-idempotent calls (e.g. payments, email) where retrying
	// could cause duplicate side effects even with an idempotency key.
	// ---------------------------------------------------------------------------
	requestCount.Store(0) // reset counter

	oneShot := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(), // sets MaxAttempts=1
	)

	fmt.Println("=== Example 2: WithDisableRetry (single attempt) ===")
	resp, err = oneShot.Execute(oneShot.Get("/data"))
	if err != nil {
		log.Fatalf("one-shot request failed: %v", err)
	}
	// The first request hits a 503 from the test server because we reset the
	// counter, and the client will NOT retry.
	fmt.Printf("Status: %d — body: %s\n", resp.StatusCode, resp.String())
	fmt.Printf("Total server hits: %d (expected 1)\n\n", requestCount.Load())

	// ---------------------------------------------------------------------------
	// 3. Per-request WithDisableRetry via client.With
	//
	// Derive a variant of an existing client that disables retries for a single
	// critical call without modifying the original client.
	// ---------------------------------------------------------------------------
	fmt.Println("=== Example 3: client.With(WithDisableRetry()) per-request ===")
	requestCount.Store(0)
	noRetryOnce := client.With(relay.WithDisableRetry())
	resp, err = noRetryOnce.Execute(noRetryOnce.Get("/data"))
	if err != nil {
		log.Fatalf("derived no-retry request failed: %v", err)
	}
	fmt.Printf("Status: %d — body: %s\n", resp.StatusCode, resp.String())
	fmt.Printf("Total server hits: %d (expected 1)\n", requestCount.Load())
}
