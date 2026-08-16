package relay

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

func TestRateLimit_AllowsRequestsWithinBurst(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	for i := 0; i < 3; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})
	}

	// 100 rps with burst=5: first 3 requests should pass immediately.
	c := New(WithRateLimit(100, 5), WithDisableRetry(), WithDisableCircuitBreaker())
	for i := 0; i < 3; i++ {
		resp, err := c.Execute(c.Get(srv.URL() + "/"))
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, resp.StatusCode)
		}
	}
}

func TestRateLimit_ContextCancelDuringWait(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	// Very low rate (0.001 rps) so the first request consumes the burst and
	// the second must wait. We cancel via a very short request timeout.
	c := New(WithRateLimit(0.001, 1), WithDisableRetry(), WithDisableCircuitBreaker())

	// First request consumes the single burst token.
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})
	if _, err := c.Execute(c.Get(srv.URL() + "/")); err != nil {
		t.Fatalf("first request: %v", err)
	}

	// Second request should fail quickly because the context will expire while
	// waiting for a token (timeout << inter-token interval of ~1000 s).
	req := c.Get(srv.URL() + "/").WithTimeout(10 * time.Millisecond)
	_, err := c.Execute(req)
	if err == nil {
		t.Error("expected error from rate limiter context cancellation")
	}
}

func TestRateLimit_TryAcquire(t *testing.T) {
	// Create a token bucket with 100 rps and burst=1.
	tb := newTokenBucket(100, 1)
	// First token should be immediately available.
	if !tb.TryAcquire() {
		t.Error("expected TryAcquire=true for fresh bucket with burst=1")
	}
	// Bucket is now empty; second should fail immediately.
	if tb.TryAcquire() {
		t.Error("expected TryAcquire=false when bucket is empty")
	}
}

// TestRateLimit_ZeroBurstDoesNotDeadlock guards against Burst: 0 (the zero
// value if a caller forgets to set it, or an explicit "no burst" attempt)
// clamping maxNanoBurst to 0, which made refill() perpetually clamp
// nanoTokens back down to 0 before it could ever reach the 1-token
// threshold Wait/TryAcquire require - the limiter deadlocked regardless of
// RequestsPerSecond. Burst < 1 must be treated as burst = 1.
func TestRateLimit_ZeroBurstDoesNotDeadlock(t *testing.T) {
	tb := newTokenBucket(1000, 0)

	if !tb.TryAcquire() {
		t.Fatal("expected TryAcquire=true with Burst=0 clamped to 1, got false (deadlocked)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := tb.Wait(ctx); err != nil {
		t.Errorf("Wait with Burst=0 (clamped to 1) should eventually succeed as tokens refill, got %v", err)
	}
}

func TestCircuitBreakerState_String(t *testing.T) {
	cases := []struct {
		state CircuitBreakerState
		want  string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			if got := tc.state.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
