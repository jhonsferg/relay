package relay_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
)

// TestResilienceStack_CombinedFeaturesUnderContextCancellation exercises
// retry, circuit breaker, hedging, rate limiting, and a small bulkhead all
// enabled simultaneously, under concurrent load engineered to actually trip
// the circuit breaker into StateOpen and then StateHalfOpen while some
// goroutines' short per-request timeouts cause acquireBulkhead to fail. Each
// resilience feature is well-tested in isolation elsewhere in this repo, but
// this class of interaction - several features combined under real
// concurrent contention and cancellation - is not, even though that is
// exactly where bugs like the circuit breaker half-open probe-slot leak
// (fixed separately, with its own precisely-orchestrated deterministic unit
// test - see TestCircuitBreaker_AbandonedHalfOpenProbeDoesNotStickForever)
// were found. This test does not reliably reproduce that specific race - the
// exact interleaving it needs is too narrow to hit consistently under
// realistic concurrent timing - so treat it as a smoke/interaction test, not
// a regression guard for that particular bug.
//
// What it does verify: nothing panics or deadlocks when these features
// interact under load, and the circuit breaker is not left permanently
// broken by the storm - a plain, uncancelled request afterward must succeed.
func TestResilienceStack_CombinedFeaturesUnderContextCancellation(t *testing.T) {
	var handled atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := handled.Add(1)
		// First third of requests: slow and failing, to reliably trip the
		// circuit breaker to StateOpen and give short-timeout callers a
		// realistic chance to time out while a bulkhead slot is held.
		if n <= 30 {
			time.Sleep(15 * time.Millisecond)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		// Remainder: fast successes, so once the breaker starts probing
		// (StateHalfOpen) it can recover instead of re-tripping forever.
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithRetry(&relay.RetryConfig{
			MaxAttempts:     2,
			InitialInterval: time.Millisecond,
			MaxInterval:     5 * time.Millisecond,
			Multiplier:      1.0,
			RandomFactor:    0,
			RetryableStatus: []int{http.StatusServiceUnavailable},
		}),
		relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
			MaxFailures:      3,                     // low: the failing first third reliably trips it
			ResetTimeout:     20 * time.Millisecond, // short: transitions to HalfOpen within the test
			HalfOpenRequests: 2,
			SuccessThreshold: 2,
		}),
		relay.WithHedgingN(15*time.Millisecond, 2),
		relay.WithRateLimit(1000, 1000), // generous: not meant to be the bottleneck
		relay.WithMaxConcurrentRequests(2),
	)
	defer func() { _ = c.Shutdown(context.Background()) }() //nolint:errcheck

	const goroutines = 60
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			req := c.Get("/")
			if i%2 == 0 {
				// Half the requests get a short timeout - combined with the
				// small bulkhead (2 slots) and slow first-third responses,
				// this reliably makes some acquireBulkhead calls fail via
				// context expiry while the breaker is StateHalfOpen.
				req = req.WithTimeout(8 * time.Millisecond)
			}
			// Errors (including context deadline exceeded, bulkhead full,
			// and circuit open) are expected here - this loop only needs to
			// confirm nothing panics or deadlocks.
			_, _ = c.Execute(req)
		}()
	}
	wg.Wait()

	// Give any in-flight background state a moment to settle, then confirm
	// the breaker recovered rather than getting stuck.
	deadline := time.Now().Add(2 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := c.Execute(c.Get("/"))
		if err == nil {
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
				t.Errorf("unexpected status after storm: %d", resp.StatusCode)
			}
			return
		}
		lastErr = err
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("client still unusable after the concurrent storm and recovery window: %v (circuit breaker state: %s)", lastErr, c.CircuitBreakerState())
}
