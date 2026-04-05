package relay

import (
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

// TestRetryBudget_AllowsUnderRatio verifies that retries are allowed when
// the retry count is below the configured ratio.
func TestRetryBudget_AllowsUnderRatio(t *testing.T) {
	tracker := newRetryBudgetTracker(RetryBudget{
		Ratio:    0.5,
		Window:   10 * time.Second,
		MinRetry: 0,
	})

	// Record 10 initial attempts.
	for i := 0; i < 10; i++ {
		tracker.RecordAttempt()
	}

	// First retry should be allowed (1/11 ~= 9% < 50%).
	if !tracker.CanRetry() {
		t.Error("expected CanRetry to return true when under ratio")
	}
}

// TestRetryBudget_BlocksWhenRatioExceeded verifies that retries are blocked
// once the retry fraction exceeds the configured ratio.
func TestRetryBudget_BlocksWhenRatioExceeded(t *testing.T) {
	tracker := newRetryBudgetTracker(RetryBudget{
		Ratio:    0.1,
		Window:   10 * time.Second,
		MinRetry: 1,
	})

	// Record 10 initial attempts.
	for i := 0; i < 10; i++ {
		tracker.RecordAttempt()
	}

	// The MinRetry=1 allows one retry.
	if !tracker.CanRetry() {
		t.Fatal("expected first retry to be allowed via MinRetry")
	}

	// Now retries=1, attempts=10, total=11.
	// ratio check: (1+1)/(11+1) = 2/12 = 16.7% > 10% - should be blocked.
	if tracker.CanRetry() {
		t.Error("expected CanRetry to return false when ratio exceeded")
	}
}

// TestRetryBudget_MinRetryAlwaysAllowed verifies that MinRetry retries are
// always granted regardless of the ratio.
func TestRetryBudget_MinRetryAlwaysAllowed(t *testing.T) {
	const minRetry = 5
	tracker := newRetryBudgetTracker(RetryBudget{
		Ratio:    0.0, // ratio=0 means no retries based on ratio
		Window:   10 * time.Second,
		MinRetry: minRetry,
	})

	// Record no initial attempts - the min should still let retries through.
	for i := 0; i < minRetry; i++ {
		if !tracker.CanRetry() {
			t.Errorf("expected MinRetry to allow retry %d, but got false", i+1)
		}
	}

	// The (minRetry+1)th retry should be blocked by ratio=0.
	if tracker.CanRetry() {
		t.Error("expected budget to block retry after MinRetry is exhausted with ratio=0")
	}
}

// TestRetryBudget_SlidingWindowEvictsOldEntries verifies that entries older
// than Window are evicted and no longer counted toward the budget.
func TestRetryBudget_SlidingWindowEvictsOldEntries(t *testing.T) {
	tracker := newRetryBudgetTracker(RetryBudget{
		Ratio:    0.0, // ratio=0, only MinRetry allows retries
		Window:   50 * time.Millisecond,
		MinRetry: 3,
	})

	// Exhaust the MinRetry budget.
	for i := 0; i < 3; i++ {
		if !tracker.CanRetry() {
			t.Fatalf("expected CanRetry true for retry %d", i+1)
		}
	}

	// Budget should now be exhausted.
	if tracker.CanRetry() {
		t.Fatal("expected budget to be exhausted")
	}

	// Wait for the window to expire so all entries are evicted.
	time.Sleep(60 * time.Millisecond)

	// After eviction, MinRetry budget should be fresh again.
	if !tracker.CanRetry() {
		t.Error("expected CanRetry true after sliding window eviction")
	}
}

// TestRetryBudget_GoroutineSafety verifies that concurrent CanRetry calls
// do not race (run with -race flag).
func TestRetryBudget_GoroutineSafety(t *testing.T) {
	tracker := newRetryBudgetTracker(RetryBudget{
		Ratio:    0.5,
		Window:   10 * time.Second,
		MinRetry: 100,
	})

	for i := 0; i < 100; i++ {
		tracker.RecordAttempt()
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tracker.CanRetry()
			tracker.RecordAttempt()
		}()
	}
	wg.Wait()
}

// TestRetryBudget_Integration verifies that a client with a RetryBudget
// limits retries across requests to an always-503 server.
func TestRetryBudget_Integration(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	// RetryConfig has MaxAttempts=3, so we enqueue enough 503s for multiple requests.
	// Each request can attempt up to 3 times; budget has MinRetry=2 so only
	// 2 retries are allowed total across all requests (ratio=0 blocks the rest).
	for i := 0; i < 20; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusServiceUnavailable})
	}

	c := New(
		WithDisableCircuitBreaker(),
		WithRetry(&RetryConfig{
			MaxAttempts:     3,
			InitialInterval: 1 * time.Millisecond,
			MaxInterval:     5 * time.Millisecond,
			Multiplier:      1.0,
			RandomFactor:    0,
			RetryableStatus: []int{http.StatusServiceUnavailable},
		}),
		WithRetryBudget(&RetryBudget{
			Ratio:    0.0, // no ratio-based retries
			Window:   10 * time.Second,
			MinRetry: 2, // only 2 retries allowed in total
		}),
	)

	// First request: initial attempt (recorded) + 2 retries (exhausts budget) = 3 calls.
	_, err1 := c.Execute(c.Get(srv.URL() + "/a"))

	// Second request: initial attempt (recorded) + budget exhausted on first retry.
	_, err2 := c.Execute(c.Get(srv.URL() + "/b"))

	// The second request's first retry should fail with ErrRetryBudgetExhausted.
	if !errors.Is(err2, ErrRetryBudgetExhausted) {
		t.Errorf("expected ErrRetryBudgetExhausted for second request, got: %v (err1=%v)", err2, err1)
	}
}

// TestRetryBudget_DefaultMinRetry verifies that MinRetry defaults to 10.
func TestRetryBudget_DefaultMinRetry(t *testing.T) {
	tracker := newRetryBudgetTracker(RetryBudget{
		Ratio:  0.0,
		Window: 10 * time.Second,
		// MinRetry not set - should default to 10
	})

	for i := 0; i < 10; i++ {
		if !tracker.CanRetry() {
			t.Errorf("expected MinRetry default of 10 to allow retry %d", i+1)
		}
	}

	if tracker.CanRetry() {
		t.Error("expected budget exhausted after default MinRetry=10")
	}
}
