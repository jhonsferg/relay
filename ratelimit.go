package relay

import (
	"context"
	"sync"
	"time"

	"github.com/jhonsferg/relay/internal/pool"
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

// nanoTokensPerToken is the fixed-point scale factor: one real token equals
// 1_000_000_000 internal "nano-token" units. This avoids float64 arithmetic on
// the hot path while preserving sub-microsecond refill precision.
const nanoTokensPerToken = int64(1_000_000_000)

// tokenBucket is a thread-safe token-bucket rate limiter. Tokens are refilled
// continuously at the configured rate up to the burst cap.
//
// Internally, token quantities are stored as integer "nano-tokens" (real tokens
// multiplied by nanoTokensPerToken). All arithmetic uses int64, eliminating
// float64 conversions and FP-unit pressure on the hot path.
type tokenBucket struct {
	// mu protects all mutable fields.
	mu sync.Mutex

	// nanoTokens is the current token balance scaled by nanoTokensPerToken.
	nanoTokens int64

	// maxNanoBurst is the maximum token capacity scaled by nanoTokensPerToken.
	maxNanoBurst int64

	// ratePerUs is the refill speed in nano-tokens per microsecond.
	// Precomputed at construction time as int64(rps * 1000).
	// Minimum value is 1 to avoid division by zero in wait calculations.
	ratePerUs int64

	// lastTime is when nanoTokens was last refilled.
	lastTime time.Time
}

// newTokenBucket constructs a token bucket pre-filled to burst capacity,
// with the given replenishment rate.
func newTokenBucket(rps float64, burst int) *tokenBucket {
	ratePerUs := int64(rps * 1000)
	if ratePerUs < 1 {
		ratePerUs = 1 // guard against very low rates (< 0.001 req/s)
	}
	maxNano := int64(burst) * nanoTokensPerToken
	return &tokenBucket{
		nanoTokens:   maxNano,
		maxNanoBurst: maxNano,
		ratePerUs:    ratePerUs,
		lastTime:     time.Now(),
	}
}

// maxRefillInterval caps the time window used for token replenishment. This
// prevents a large token burst after the process is paused (e.g. a VM
// snapshot/resume or a debugger breakpoint), which would allow a stampede of
// requests that violates the configured rate.
const maxRefillInterval = 5 * time.Second

// maxRefillUs is maxRefillInterval expressed in microseconds for use in the
// integer refill arithmetic.
var maxRefillUs = maxRefillInterval.Microseconds()

// refill adds nano-tokens proportional to elapsed microseconds since the last
// call, capped at maxNanoBurst. Must be called with tb.mu held.
func (tb *tokenBucket) refill() {
	now := time.Now()
	elapsedUs := now.Sub(tb.lastTime).Microseconds()
	if elapsedUs > maxRefillUs {
		elapsedUs = maxRefillUs
	}
	tb.nanoTokens += elapsedUs * tb.ratePerUs
	if tb.nanoTokens > tb.maxNanoBurst {
		tb.nanoTokens = tb.maxNanoBurst
	}
	tb.lastTime = now
}

// Wait blocks until a token is available or ctx is done. It returns ctx.Err()
// if the context is canceled or its deadline expires while waiting.
func (tb *tokenBucket) Wait(ctx context.Context) error {
	for {
		tb.mu.Lock()
		tb.refill()
		if tb.nanoTokens >= nanoTokensPerToken {
			tb.nanoTokens -= nanoTokensPerToken
			tb.mu.Unlock()
			return nil
		}
		// Compute wait duration: ceil((deficit) / ratePerUs) microseconds.
		deficit := nanoTokensPerToken - tb.nanoTokens
		waitUs := (deficit + tb.ratePerUs - 1) / tb.ratePerUs
		tb.mu.Unlock()

		timer := pool.GetTimer(time.Duration(waitUs) * time.Microsecond)
		select {
		case <-ctx.Done():
			pool.PutTimer(timer)
			return ctx.Err()
		case <-timer.C:
			pool.PutTimer(timer)
		}
	}
}

// TryAcquire attempts to consume one token without blocking. Returns true if a
// token was available and consumed, false if the bucket is empty.
func (tb *tokenBucket) TryAcquire() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.refill()
	if tb.nanoTokens >= nanoTokensPerToken {
		tb.nanoTokens -= nanoTokensPerToken
		return true
	}
	return false
}
