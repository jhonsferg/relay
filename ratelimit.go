package relay

import (
	"context"
	"sync"
	"time"
)

// RateLimitConfig configures the client-side token-bucket rate limiter.
type RateLimitConfig struct {
	// RequestsPerSecond is the sustained token replenishment rate, i.e. the
	// long-term maximum number of requests per second.
	RequestsPerSecond float64

	// Burst is the maximum number of tokens that can accumulate above the
	// sustained rate. A burst of N allows N requests to be dispatched
	// immediately before throttling begins.
	Burst int
}

// tokenBucket is a thread-safe token-bucket rate limiter. Tokens are refilled
// continuously at the configured rate up to the burst cap.
type tokenBucket struct {
	// mu protects all mutable fields.
	mu sync.Mutex

	// tokens is the current number of available tokens.
	tokens float64

	// maxBurst is the maximum token capacity (equals Burst at construction).
	maxBurst float64

	// rate is the token replenishment speed in tokens per second.
	rate float64

	// lastTime is when tokens was last refilled. Used to compute elapsed time.
	lastTime time.Time
}

// newTokenBucket constructs a token bucket pre-filled to burst capacity,
// with the given replenishment rate.
func newTokenBucket(rps float64, burst int) *tokenBucket {
	return &tokenBucket{
		tokens:   float64(burst),
		maxBurst: float64(burst),
		rate:     rps,
		lastTime: time.Now(),
	}
}

// maxRefillInterval caps the time window used for token replenishment. This
// prevents a large token burst after the process is paused (e.g. a VM
// snapshot/resume or a debugger breakpoint), which would allow a stampede of
// requests that violates the configured rate.
const maxRefillInterval = 5 * time.Second

// refill adds tokens proportional to the time elapsed since the last call,
// capped at maxBurst. The elapsed time is capped at maxRefillInterval to
// prevent burst accumulation after long pauses. Must be called with tb.mu held.
func (tb *tokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(tb.lastTime)
	if elapsed > maxRefillInterval {
		elapsed = maxRefillInterval
	}
	tb.tokens += elapsed.Seconds() * tb.rate
	if tb.tokens > tb.maxBurst {
		tb.tokens = tb.maxBurst
	}
	tb.lastTime = now
}

// Wait blocks until a token is available or ctx is done. It returns ctx.Err()
// if the context is canceled or its deadline expires while waiting.
func (tb *tokenBucket) Wait(ctx context.Context) error {
	for {
		tb.mu.Lock()
		tb.refill()
		if tb.tokens >= 1 {
			tb.tokens--
			tb.mu.Unlock()
			return nil
		}
		// Compute how long to wait for the next token to become available.
		wait := time.Duration((1 - tb.tokens) / tb.rate * float64(time.Second))
		tb.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// TryAcquire attempts to consume one token without blocking. Returns true if a
// token was available and consumed, false if the bucket is empty.
func (tb *tokenBucket) TryAcquire() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}
	return false
}
