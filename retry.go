package relay

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jhonsferg/relay/internal/backoff"
	"github.com/jhonsferg/relay/internal/pool"
)

// RetryConfig controls the retry and backoff behaviour of the client.
// The default policy retries on network errors and 5xx/429 responses using
// exponential backoff with full jitter.
type RetryConfig struct {
	// MaxAttempts is the total number of tries, including the initial one.
	// Set to 1 to disable retries.
	MaxAttempts int

	// InitialInterval is the base delay before the first retry.
	InitialInterval time.Duration

	// MaxInterval caps the computed backoff delay regardless of how many
	// attempts have been made.
	MaxInterval time.Duration

	// Multiplier grows the interval on each successive attempt.
	// 2.0 produces classic exponential backoff.
	Multiplier float64

	// RandomFactor adds ±jitter proportional to the computed interval.
	// 0 disables jitter entirely; 0.5 adds up to ±50 % random jitter.
	RandomFactor float64

	// RetryableStatus is the set of HTTP status codes that trigger a retry.
	// Defaults to [429, 500, 502, 503, 504].
	RetryableStatus []int

	// RetryIf is an optional predicate called when the built-in logic would
	// retry. Returning false prevents the retry even when the status or error
	// is normally retryable. Either argument may be nil depending on whether
	// the trigger was an HTTP status code or a transport error.
	//
	//	RetryIf: func(resp *http.Response, err error) bool {
	//	    if resp != nil { return resp.StatusCode == 503 }
	//	    return true
	//	}
	RetryIf func(resp *http.Response, err error) bool

	// OnRetry is an optional callback invoked before each retry sleep.
	// attempt is 1-based (first retry = 1). Useful for structured logging.
	// Either resp or err may be nil depending on the failure mode.
	OnRetry func(attempt int, resp *http.Response, err error)
}

// defaultRetryConfig returns a RetryConfig with sensible production defaults:
// three attempts, 100 ms initial interval, exponential backoff (×2) with
// ±50 % jitter, capped at 30 s, retrying on [429, 500, 502, 503, 504].
func defaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts:     3,
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     30 * time.Second,
		Multiplier:      2.0,
		RandomFactor:    0.5,
		RetryableStatus: []int{
			http.StatusTooManyRequests,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout,
		},
	}
}

// retrier wraps a RetryConfig and executes the retry loop around each HTTP
// call via [retrier.Do].
type retrier struct {
	cfg    *RetryConfig
	budget *retryBudgetTracker
}

// newRetrier constructs a retrier from cfg. If cfg is nil the default config
// is used. MaxAttempts is clamped to a minimum of 1.
func newRetrier(cfg *RetryConfig) *retrier {
	if cfg == nil {
		cfg = defaultRetryConfig()
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	return &retrier{cfg: cfg}
}

// isRetryableStatus reports whether the HTTP status code is in the configured
// retryable set.
func (r *retrier) isRetryableStatus(code int) bool {
	for _, s := range r.cfg.RetryableStatus {
		if s == code {
			return true
		}
	}
	return false
}

// isRetryableErr reports whether a transport-level error warrants a retry
// attempt. Context cancellation and deadline expiry are never retried because
// those represent explicit caller intent.
func (r *retrier) isRetryableErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}

// backoff computes the sleep duration before attempt n (0-based).
// Delegates to internal/backoff for a single, canonical implementation.
func (r *retrier) backoff(attempt int) time.Duration {
	return backoff.Config{
		InitialInterval: r.cfg.InitialInterval,
		MaxInterval:     r.cfg.MaxInterval,
		Multiplier:      r.cfg.Multiplier,
		RandomFactor:    r.cfg.RandomFactor,
	}.Next(attempt)
}

// retryAfterDelay parses the Retry-After response header and returns the
// indicated delay. Supports both the delay-seconds form ("120") and the
// HTTP-date form. Returns 0 if the header is absent or unparseable.
func (r *retrier) retryAfterDelay(resp *http.Response) time.Duration {
	val := resp.Header.Get("Retry-After")
	if val == "" {
		return 0
	}
	if secs, err := strconv.Atoi(val); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(val); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// isIdempotentMethod reports whether the HTTP method is idempotent per RFC 7231.
// Idempotent methods can be safely retried without risk of duplicate side effects.
// POST and PATCH are not idempotent and are excluded from automatic retries by default.
func isIdempotentMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut,
		http.MethodDelete, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}

// Do executes fn up to MaxAttempts times. Between attempts it sleeps for the
// computed backoff duration (or the Retry-After header value for HTTP 429).
// Returns the first successful response or the last error encountered.
//
// method is the HTTP verb of the request (used to guard against retrying
// non-idempotent operations). hasIdempotencyKey must be true when the caller
// has set an X-Idempotency-Key header, which signals that the server handles
// deduplication and makes even POST/PATCH safe to retry.
//
// Non-idempotent methods (POST, PATCH) without an idempotency key are not
// retried by default. Set RetryIf to explicitly allow retries for them.
func (r *retrier) Do(ctx context.Context, method string, hasIdempotencyKey bool, fn func() (*http.Response, error)) (*http.Response, error) {
	safeToRetry := isIdempotentMethod(method) || hasIdempotencyKey
	var (
		lastErr     error
		pendingWait time.Duration
	)

	// Record the initial (non-retry) attempt for budget accounting.
	if r.budget != nil {
		r.budget.RecordAttempt()
	}

	for attempt := 0; attempt < r.cfg.MaxAttempts; attempt++ {
		if attempt > 0 {
			// Check retry budget before sleeping and retrying.
			if r.budget != nil && !r.budget.CanRetry() {
				if lastErr != nil {
					return nil, ErrRetryBudgetExhausted
				}
				// Budget exhausted after a retryable status - return budget error.
				return nil, ErrRetryBudgetExhausted
			}

			wait := pendingWait
			if wait == 0 {
				wait = r.backoff(attempt - 1)
			}
			timer := pool.GetTimer(wait)
			select {
			case <-ctx.Done():
				pool.PutTimer(timer)
				return nil, ctx.Err()
			case <-timer.C:
				pool.PutTimer(timer)
			}
		}

		pendingWait = 0
		resp, err := fn()

		if err != nil {
			if !r.isRetryableErr(err) {
				return nil, err
			}
			// Non-idempotent methods without an idempotency key are not retried
			// on transport errors unless the caller explicitly opts in via RetryIf.
			if !safeToRetry && r.cfg.RetryIf == nil {
				return nil, err
			}
			if r.cfg.RetryIf != nil && !r.cfg.RetryIf(nil, err) {
				return nil, err
			}
			if r.cfg.OnRetry != nil && attempt < r.cfg.MaxAttempts-1 {
				r.cfg.OnRetry(attempt+1, nil, err)
			}
			lastErr = err
			continue
		}

		if !r.isRetryableStatus(resp.StatusCode) {
			return resp, nil
		}

		// Non-idempotent methods without an idempotency key are not retried by
		// default to prevent duplicate side effects. Callers can override this
		// by setting RetryIf or by attaching an idempotency key to the request.
		if !safeToRetry && r.cfg.RetryIf == nil {
			return resp, nil
		}

		// Custom predicate may opt out of retrying this particular response.
		if r.cfg.RetryIf != nil && !r.cfg.RetryIf(resp, nil) {
			return resp, nil
		}

		// On the last attempt return the response as-is instead of discarding it.
		if attempt == r.cfg.MaxAttempts-1 {
			return resp, nil
		}

		if r.cfg.OnRetry != nil {
			r.cfg.OnRetry(attempt+1, resp, nil)
		}

		// Respect the Retry-After header (e.g. on 429 Too Many Requests).
		pendingWait = r.retryAfterDelay(resp)
		_ = resp.Body.Close() //nolint:errcheck
	}

	if lastErr != nil {
		return nil, lastErr
	}

	return nil, ErrMaxRetriesReached
}
