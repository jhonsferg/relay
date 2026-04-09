package relay

import (
	"errors"
	"sync"
	"time"
)

// ErrRetryBudgetExhausted is returned when the retry budget is exceeded.
var ErrRetryBudgetExhausted = errors.New("relay: retry budget exhausted")

// RetryBudget limits retries using a sliding window token bucket.
type RetryBudget struct {
	// Ratio is the maximum fraction of requests that can be retried.
	// E.g., 0.1 means at most 10% of requests in the window can be retried.
	Ratio float64
	// Window is the sliding window duration.
	Window time.Duration
	// MinRetry is the minimum number of retries always allowed regardless of ratio.
	// Default 10.
	MinRetry int
}

// retryBudgetTracker is the runtime state for a RetryBudget.
type retryBudgetTracker struct {
	budget  RetryBudget
	mu      sync.Mutex
	entries []budgetEntry
}

type budgetEntry struct {
	ts      time.Time
	isRetry bool
}

func newRetryBudgetTracker(b RetryBudget) *retryBudgetTracker {
	if b.MinRetry <= 0 {
		b.MinRetry = 10
	}
	return &retryBudgetTracker{budget: b}
}

// RecordAttempt records a new initial attempt (not a retry).
func (t *retryBudgetTracker) RecordAttempt() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = append(t.entries, budgetEntry{ts: time.Now(), isRetry: false})
	t.evict()
}

// CanRetry checks if a retry is allowed and records it if so.
func (t *retryBudgetTracker) CanRetry() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.evict()

	var attempts, retries int
	for _, e := range t.entries {
		if e.isRetry {
			retries++
		} else {
			attempts++
		}
	}

	// MinRetry always allowed regardless of ratio.
	if retries < t.budget.MinRetry {
		t.entries = append(t.entries, budgetEntry{ts: time.Now(), isRetry: true})
		return true
	}

	// Ratio check: would adding one more retry exceed the budget?
	total := attempts + retries
	if total == 0 || float64(retries+1)/float64(total+1) <= t.budget.Ratio {
		t.entries = append(t.entries, budgetEntry{ts: time.Now(), isRetry: true})
		return true
	}

	return false
}

// evict removes entries older than the sliding window. Must be called with mu held.
// When a significant portion of capacity is wasted by the slice header advance,
// the live entries are copied to a fresh slice to release the backing array.
func (t *retryBudgetTracker) evict() {
	cutoff := time.Now().Add(-t.budget.Window)
	i := 0
	for i < len(t.entries) && t.entries[i].ts.Before(cutoff) {
		i++
	}
	if i == 0 {
		return
	}
	remaining := t.entries[i:]
	// If more than half the backing array is dead space, compact to avoid leaking memory.
	if i > len(remaining) {
		fresh := make([]budgetEntry, len(remaining))
		copy(fresh, remaining)
		t.entries = fresh
	} else {
		t.entries = remaining
	}
}
