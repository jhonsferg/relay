package relay

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

// newFastBreakerClient returns a client with a circuit breaker that trips after
// maxFailures failures and resets after resetTimeout. Retries are disabled so
// each HTTP call is a single attempt.
func newFastBreakerClient(maxFailures int, resetTimeout time.Duration, onStateChange func(from, to CircuitBreakerState)) *Client {
	return New(
		WithDisableRetry(),
		WithCircuitBreaker(&CircuitBreakerConfig{
			MaxFailures:      maxFailures,
			ResetTimeout:     resetTimeout,
			HalfOpenRequests: 3,
			SuccessThreshold: 2,
			OnStateChange:    onStateChange,
		}),
	)
}

func TestCircuitBreaker_ClosedToOpen(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	// Enqueue enough 500s to trip the circuit (maxFailures=3).
	for i := 0; i < 10; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	}

	c := newFastBreakerClient(3, time.Hour, nil)

	// Send 3 failing requests to trip the breaker.
	for i := 0; i < 3; i++ {
		_, _ = c.Execute(c.Get(srv.URL() + "/"))
	}

	if c.CircuitBreakerState() != StateOpen {
		t.Errorf("expected StateOpen after %d failures, got %s", 3, c.CircuitBreakerState())
	}
}

func TestCircuitBreaker_OpenRejectsRequests(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	for i := 0; i < 10; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	}

	c := newFastBreakerClient(2, time.Hour, nil)

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		c.Execute(c.Get(srv.URL() + "/")) //nolint:errcheck
	}

	if c.CircuitBreakerState() != StateOpen {
		t.Fatal("circuit breaker should be open")
	}

	countBefore := srv.RequestCount()
	_, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != ErrCircuitOpen {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}
	// No additional request should have reached the server.
	if srv.RequestCount() != countBefore {
		t.Errorf("open circuit should not send requests to server")
	}
}

func TestCircuitBreaker_OpenToHalfOpen(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	resetTimeout := 30 * time.Millisecond
	c := newFastBreakerClient(2, resetTimeout, nil)

	// Enqueue exactly 2 failures to trip the breaker, then successes for the probe.
	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	// Trip the breaker with exactly maxFailures=2 failures.
	for i := 0; i < 2; i++ {
		c.Execute(c.Get(srv.URL() + "/")) //nolint:errcheck
	}

	if c.CircuitBreakerState() != StateOpen {
		t.Fatal("breaker should be open")
	}

	// Wait for reset timeout to elapse.
	time.Sleep(resetTimeout + 20*time.Millisecond)

	// Next Allow() call should transition to half-open; Execute triggers that.
	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("after reset timeout Execute should succeed, got %v", err)
	}
	_ = resp

	state := c.CircuitBreakerState()
	if state == StateOpen {
		t.Errorf("breaker should no longer be open after reset timeout, got %s", state)
	}
}

func TestCircuitBreaker_HalfOpenToClosedOnSuccesses(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	resetTimeout := 20 * time.Millisecond
	c := newFastBreakerClient(2, resetTimeout, nil)

	// Enqueue exactly 2 failures (to trip), then 2 successes (SuccessThreshold=2).
	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	// Trip breaker with exactly maxFailures=2 failures.
	for i := 0; i < 2; i++ {
		c.Execute(c.Get(srv.URL() + "/")) //nolint:errcheck
	}

	if c.CircuitBreakerState() != StateOpen {
		t.Fatal("breaker should be open after 2 failures")
	}

	// Wait for reset timeout.
	time.Sleep(resetTimeout + 20*time.Millisecond)

	// Send SuccessThreshold (2) successful requests.
	for i := 0; i < 2; i++ {
		_, err := c.Execute(c.Get(srv.URL() + "/"))
		if err != nil {
			t.Fatalf("half-open probe %d failed: %v", i, err)
		}
	}

	if c.CircuitBreakerState() != StateClosed {
		t.Errorf("expected StateClosed after %d successes in half-open, got %s", 2, c.CircuitBreakerState())
	}
}

func TestCircuitBreaker_HalfOpenToOpenOnFailure(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	resetTimeout := 20 * time.Millisecond
	c := newFastBreakerClient(2, resetTimeout, nil)

	// Enqueue exactly 2 failures to trip, then 1 failure for the half-open probe.
	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		c.Execute(c.Get(srv.URL() + "/")) //nolint:errcheck
	}

	if c.CircuitBreakerState() != StateOpen {
		t.Fatal("breaker should be open after 2 failures")
	}

	// Wait for reset.
	time.Sleep(resetTimeout + 20*time.Millisecond)

	// Send a failing request in half-open: should re-open.
	c.Execute(c.Get(srv.URL() + "/")) //nolint:errcheck

	if c.CircuitBreakerState() != StateOpen {
		t.Errorf("expected StateOpen after failure in half-open, got %s", c.CircuitBreakerState())
	}
}

func TestCircuitBreaker_ResetCircuitBreaker(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	// Exactly 2 failures to trip (maxFailures=2), then a success after reset.
	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := newFastBreakerClient(2, time.Hour, nil)

	// Trip the breaker.
	for i := 0; i < 2; i++ {
		c.Execute(c.Get(srv.URL() + "/")) //nolint:errcheck
	}

	if c.CircuitBreakerState() != StateOpen {
		t.Fatal("breaker should be open")
	}

	// Manual reset.
	c.ResetCircuitBreaker()

	if c.CircuitBreakerState() != StateClosed {
		t.Errorf("expected StateClosed after manual reset, got %s", c.CircuitBreakerState())
	}

	// Requests should flow again.
	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute after reset: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after reset, got %d", resp.StatusCode)
	}
}

func TestCircuitBreaker_OnStateChangeCallback(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	for i := 0; i < 10; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	}

	type transition struct{ from, to CircuitBreakerState }
	transitions := make([]transition, 0)
	mu := make(chan struct{}, 16)

	c := newFastBreakerClient(2, time.Hour, func(from, to CircuitBreakerState) {
		transitions = append(transitions, transition{from, to})
		mu <- struct{}{}
	})

	// Trip the breaker (2 failures → Open).
	for i := 0; i < 2; i++ {
		c.Execute(c.Get(srv.URL() + "/")) //nolint:errcheck
	}

	// Wait for the callback.
	select {
	case <-mu:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for OnStateChange callback")
	}

	if len(transitions) == 0 {
		t.Fatal("expected at least one state transition")
	}
	last := transitions[len(transitions)-1]
	if last.from != StateClosed || last.to != StateOpen {
		t.Errorf("expected Closed→Open transition, got %s→%s", last.from, last.to)
	}
}

func TestCircuitBreaker_DirectStateManipulation(t *testing.T) {
	cb := newCircuitBreaker(&CircuitBreakerConfig{
		MaxFailures:      3,
		ResetTimeout:     1 * time.Millisecond,
		HalfOpenRequests: 2,
		SuccessThreshold: 2,
	})

	if cb.State() != StateClosed {
		t.Errorf("initial state should be Closed, got %s", cb.State())
	}

	// Record failures to trip.
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Errorf("expected Open after 3 failures, got %s", cb.State())
	}

	// Wait for reset timeout.
	time.Sleep(10 * time.Millisecond)

	// Allow() transitions to HalfOpen.
	if !cb.Allow() {
		t.Error("Allow() should return true after reset timeout")
	}
	if cb.State() != StateHalfOpen {
		t.Errorf("expected HalfOpen after reset timeout, got %s", cb.State())
	}

	// Two successes close it.
	cb.RecordSuccess()
	cb.RecordSuccess()
	if cb.State() != StateClosed {
		t.Errorf("expected Closed after 2 successes in HalfOpen, got %s", cb.State())
	}
}

func TestCircuitBreaker_ResetFromOpenClearsCounters(t *testing.T) {
	cb := newCircuitBreaker(&CircuitBreakerConfig{
		MaxFailures:      2,
		ResetTimeout:     time.Hour,
		HalfOpenRequests: 1,
		SuccessThreshold: 1,
	})

	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatal("expected Open")
	}

	cb.Reset()
	if cb.State() != StateClosed {
		t.Errorf("expected Closed after Reset, got %s", cb.State())
	}
	if !cb.Allow() {
		t.Error("Allow() should return true after Reset")
	}
}

func TestCircuitBreaker_IsHealthy(t *testing.T) {
	cb := newCircuitBreaker(&CircuitBreakerConfig{
		MaxFailures:      1,
		ResetTimeout:     time.Hour,
		HalfOpenRequests: 1,
		SuccessThreshold: 1,
	})

	c := &Client{circuitBreaker: cb}
	if !c.IsHealthy() {
		t.Error("should be healthy when closed")
	}

	cb.RecordFailure()
	if c.IsHealthy() {
		t.Error("should not be healthy when open")
	}
}

// TestWithDisableCircuitBreaker_NoBreakerBuilt guards against a regression
// where buildClient called newCircuitBreaker(nil) unconditionally: since
// newCircuitBreaker falls back to defaultCircuitBreakerConfig on a nil
// argument, that produced a live breaker (5-failure default threshold) even
// though the caller asked to disable it entirely.
func TestWithDisableCircuitBreaker_NoBreakerBuilt(t *testing.T) {
	c := New(WithDisableCircuitBreaker())
	if c.circuitBreaker != nil {
		t.Error("WithDisableCircuitBreaker should leave c.circuitBreaker nil, not a default-configured breaker")
	}
}

// TestCircuitBreaker_AbandonedHalfOpenProbeDoesNotStickForever guards against
// a regression where a half-open probe slot granted by Allow() leaked
// permanently when the request was rejected downstream (by the bulkhead)
// before it could reach RecordSuccess/RecordFailure. Without releasing the
// slot, HalfOpenRequests eventually saturates with abandoned probes and the
// breaker stops admitting any further recovery attempt - stuck in
// StateHalfOpen forever, since there is no StateOpen-style auto-recovery
// timeout for that state.
func TestCircuitBreaker_AbandonedHalfOpenProbeDoesNotStickForever(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	resetTimeout := 10 * time.Millisecond
	c := New(
		WithDisableRetry(),
		WithMaxConcurrentRequests(1),
		WithCircuitBreaker(&CircuitBreakerConfig{
			MaxFailures:      1,
			ResetTimeout:     resetTimeout,
			HalfOpenRequests: 1,
			SuccessThreshold: 1,
		}),
	)
	cb := c.circuitBreaker

	// Trip the breaker directly, bypassing HTTP.
	cb.RecordFailure()
	if cb.State() != StateOpen {
		t.Fatal("breaker should be open")
	}
	time.Sleep(resetTimeout + 20*time.Millisecond)

	// Consume the implicit Open->HalfOpen transition slot (Allow() grants
	// this one without incrementing halfOpenRequests), so the *next* Allow()
	// call is the one that actually reserves a tracked probe slot.
	if !cb.Allow() {
		t.Fatal("Allow() should transition Open->HalfOpen and return true")
	}
	if cb.State() != StateHalfOpen {
		t.Fatalf("expected StateHalfOpen, got %s", cb.State())
	}

	// Saturate the sole bulkhead slot so the next Execute's acquireBulkhead
	// call is guaranteed to fail via context cancellation.
	releaseBulkhead, err := c.acquireBulkhead(context.Background(), c.Get(srv.URL()+"/"))
	if err != nil {
		t.Fatalf("failed to saturate bulkhead: %v", err)
	}

	// This request's Allow() call reserves the one tracked half-open probe
	// slot, but acquireBulkhead fails immediately because the bulkhead is
	// saturated and the request's own timeout is very short.
	_, err = c.Execute(c.Get(srv.URL() + "/").WithTimeout(5 * time.Millisecond))
	if err == nil {
		t.Fatal("expected acquireBulkhead to fail while the bulkhead is saturated")
	}

	releaseBulkhead()

	// With the fix, the abandoned probe slot was released, so the breaker
	// can still admit a probe. Without it, halfOpenRequests stays pinned at
	// the HalfOpenRequests limit (1) and every future Allow() call in
	// StateHalfOpen returns false forever.
	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("breaker should still admit a probe after the abandoned attempt, got err: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestCircuitBreaker_DisabledNeverTrips is the behavioural counterpart to
// TestWithDisableCircuitBreaker_NoBreakerBuilt: it drives more consecutive
// failures than the default MaxFailures (5) through a disabled breaker and
// asserts it never opens and never rejects a request with ErrCircuitOpen.
func TestCircuitBreaker_DisabledNeverTrips(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	for i := 0; i < 10; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	}

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())

	for i := 0; i < 10; i++ {
		_, err := c.Execute(c.Get(srv.URL() + "/"))
		if err == ErrCircuitOpen {
			t.Fatalf("request %d: circuit breaker tripped despite WithDisableCircuitBreaker", i)
		}
	}

	if c.CircuitBreakerState() != StateClosed {
		t.Errorf("expected StateClosed with circuit breaker disabled, got %s", c.CircuitBreakerState())
	}
	if got := srv.RequestCount(); got != 10 {
		t.Errorf("expected all 10 requests to reach the server, got %d", got)
	}
}
