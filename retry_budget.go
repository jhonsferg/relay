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

	// attempts and retries are running counts of live (not yet evicted)
	// entries, maintained incrementally so CanRetry doesn't need to rescan
	// the whole entries slice on every call - see evict/CanRetry/RecordAttempt.
	attempts int
	retries  int
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
	t.attempts++
	t.evict()
}

// CanRetry checks if a retry is allowed and records it if so.
//
// attempts/retries are maintained incrementally (see evict) rather than
// recounted from entries here: under sustained traffic, entries holds one
// entry per request within Window, so a full rescan on every CanRetry call
// would cost O(requests in window) - serialising all concurrent retry-budget
// checks behind an increasingly expensive scan as traffic or Window grows.
// Incremental counters make this O(1) plus the amortised O(1) eviction cost
// per call (each entry is evicted exactly once over its lifetime).
func (t *retryBudgetTracker) CanRetry() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.evict()

	// MinRetry always allowed regardless of ratio.
	if t.retries < t.budget.MinRetry {
		t.entries = append(t.entries, budgetEntry{ts: time.Now(), isRetry: true})
		t.retries++
		return true
	}

	// Ratio check: would adding one more retry exceed the budget?
	total := t.attempts + t.retries
	if total == 0 || float64(t.retries+1)/float64(total+1) <= t.budget.Ratio {
		t.entries = append(t.entries, budgetEntry{ts: time.Now(), isRetry: true})
		t.retries++
		return true
	}

	return false
}

// evict removes entries older than the sliding window, decrementing
// attempts/retries to match. Must be called with mu held. When a
// significant portion of capacity is wasted by the slice header advance,
// the live entries are copied to a fresh slice to release the backing array.
func (t *retryBudgetTracker) evict() {
	cutoff := time.Now().Add(-t.budget.Window)
	i := 0
	for i < len(t.entries) && t.entries[i].ts.Before(cutoff) {
		if t.entries[i].isRetry {
			t.retries--
		} else {
			t.attempts--
		}
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
