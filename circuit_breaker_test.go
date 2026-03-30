package relay

import (
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
