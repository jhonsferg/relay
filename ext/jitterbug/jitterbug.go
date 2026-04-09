// Package jitterbug provides alternative retry strategies for the relay HTTP
// client. The built-in relay retry uses exponential backoff with full jitter.
// This package adds:
//
//   - [WithDecorrelatedJitter] - AWS-recommended decorrelated jitter
//   - [WithLinearBackoff] - linearly growing delay with optional jitter
//   - [WithRetryBudget] - hard total-time ceiling across all attempts
//
// All strategies implement an [http.RoundTripper] transport middleware and plug
// in via [relay.WithTransportMiddleware]. Because they manage their own retry
// loop you should pair them with [relay.WithDisableRetry] to prevent
// double-retrying:
//
//	client := relay.New(
//	    relay.WithDisableRetry(),
//	    jitterbug.WithDecorrelatedJitter(jitterbug.Config{MaxAttempts: 5}),
//	)
//
// # Body replay
//
// Each strategy buffers the request body before the first attempt so
// subsequent retries can restore it. For bodies that already implement
// [http.Request.GetBody] (e.g. those created with json / form helpers) the
// original GetBody function is used instead, avoiding the extra allocation.
package jitterbug

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/jhonsferg/relay"
)

// Config is the shared configuration for all jitterbug strategies.
type Config struct {
	// MaxAttempts is the total number of tries including the initial one.
	// Zero is treated as 3.
	MaxAttempts int

	// Base is the minimum sleep duration and the starting point for all
	// backoff formulas. Zero is treated as 100 ms.
	Base time.Duration

	// Cap is the maximum sleep for a single inter-attempt pause.
	// Zero is treated as 30 s.
	Cap time.Duration

	// RetryableStatus lists the HTTP status codes that trigger a retry.
	// Nil defaults to {429, 500, 502, 503, 504}.
	RetryableStatus []int

	// RetryIf is called when the built-in logic would retry. Return false to
	// suppress the retry. Either argument may be nil.
	RetryIf func(resp *http.Response, err error) bool

	// OnRetry is called before each retry sleep. attempt is 1-based.
	OnRetry func(attempt int, resp *http.Response, err error)
}

func (c *Config) applyDefaults() {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 3
	}
	if c.Base <= 0 {
		c.Base = 100 * time.Millisecond
	}
	if c.Cap <= 0 {
		c.Cap = 30 * time.Second
	}
	if len(c.RetryableStatus) == 0 {
		c.RetryableStatus = []int{
			http.StatusTooManyRequests,
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusGatewayTimeout,
		}
	}
}

// base transport embedded by all three strategy types.
type baseTransport struct {
	next http.RoundTripper
	cfg  Config
}

// isRetryable reports whether resp+err warrant another attempt.
func (t *baseTransport) isRetryable(resp *http.Response, err error) bool {
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false
		}
		if t.cfg.RetryIf != nil {
			return t.cfg.RetryIf(nil, err)
		}
		return true
	}
	for _, s := range t.cfg.RetryableStatus {
		if resp != nil && resp.StatusCode == s {
			if t.cfg.RetryIf != nil {
				return t.cfg.RetryIf(resp, nil)
			}
			return true
		}
	}
	return false
}

// prepareBody ensures req.GetBody is set so retries can restore the body.
// If GetBody is already set (e.g. for bytes/strings bodies) it is left as-is.
// Otherwise the body is drained once and a closure is attached.
func prepareBody(req *http.Request) error {
	if req.Body == nil || req.Body == http.NoBody {
		return nil
	}
	if req.GetBody != nil {
		return nil
	}
	data, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(data))
	req.Body = io.NopCloser(bytes.NewReader(data))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	return nil
}

// restoreBody replaces req.Body with a fresh reader for the next attempt.
func restoreBody(req *http.Request) error {
	if req.GetBody == nil {
		return nil
	}
	body, err := req.GetBody()
	if err != nil {
		return err
	}
	req.Body = body
	return nil
}

// pause waits for d or returns ctx.Err() if the context expires first.
func pause(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// doRetryLoop is the shared retry orchestrator. backoffFn returns the sleep
// duration before attempt n (1-based, so n=1 is the first retry).
func (t *baseTransport) doRetryLoop(
	req *http.Request,
	backoffFn func(attempt int) time.Duration,
) (*http.Response, error) {
	if err := prepareBody(req); err != nil {
		return nil, err
	}

	var lastErr error

	for attempt := 0; attempt < t.cfg.MaxAttempts; attempt++ {
		if attempt > 0 {
			if err := restoreBody(req); err != nil {
				return nil, err
			}
			if err := pause(req.Context(), backoffFn(attempt)); err != nil {
				return nil, err
			}
		}

		resp, err := t.next.RoundTrip(req)

		if !t.isRetryable(resp, err) {
			return resp, err
		}

		// Last attempt: return whatever we got.
		if attempt == t.cfg.MaxAttempts-1 {
			if resp != nil {
				return resp, nil
			}
			if err != nil {
				return nil, err
			}
		}

		if t.cfg.OnRetry != nil {
			t.cfg.OnRetry(attempt+1, resp, err)
		}
		if resp != nil {
			// Drain up to 4 KiB so the underlying TCP connection can be reused.
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
		}
		lastErr = err
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, relay.ErrMaxRetriesReached
}

// ---------------------------------------------------------------------------
// Decorrelated Jitter
// ---------------------------------------------------------------------------

// WithDecorrelatedJitter returns a [relay.Option] that retries using the
// decorrelated jitter algorithm recommended by AWS:
//
//	sleep = clamp(base, cap, rand(base, prev_sleep × 3))
//
// Compared to full-jitter exponential backoff this spreads concurrent retries
// more evenly across time, reducing thundering-herd contention.
//
// Pair with [relay.WithDisableRetry] to avoid double-retrying.
func WithDecorrelatedJitter(cfg Config) relay.Option {
	cfg.applyDefaults()
	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &decorrelatedTransport{
			baseTransport: baseTransport{next: next, cfg: cfg},
		}
	})
}

type decorrelatedTransport struct {
	baseTransport
}

func (t *decorrelatedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	prevSleep := t.cfg.Base

	return t.doRetryLoop(req, func(attempt int) time.Duration {
		upper := prevSleep * 3
		if upper > t.cfg.Cap {
			upper = t.cfg.Cap
		}
		lo := int64(t.cfg.Base)
		hi := int64(upper)
		if hi <= lo {
			hi = lo + 1
		}
		d := time.Duration(lo + rand.Int64N(hi-lo))
		prevSleep = d
		return d
	})
}

// ---------------------------------------------------------------------------
// Linear Backoff
// ---------------------------------------------------------------------------

// WithLinearBackoff returns a [relay.Option] that retries with linearly growing
// delays: sleep = min(cap, base × attempt). An optional jitterFactor (0–1)
// adds up to jitterFactor×sleep of random noise to each pause.
//
// Pair with [relay.WithDisableRetry] to avoid double-retrying.
func WithLinearBackoff(cfg Config, jitterFactor float64) relay.Option {
	cfg.applyDefaults()
	if jitterFactor < 0 {
		jitterFactor = 0
	}
	if jitterFactor > 1 {
		jitterFactor = 1
	}
	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &linearTransport{
			baseTransport: baseTransport{next: next, cfg: cfg},
			jitterFactor:  jitterFactor,
		}
	})
}

type linearTransport struct {
	baseTransport
	jitterFactor float64
}

func (t *linearTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.doRetryLoop(req, func(attempt int) time.Duration {
		d := time.Duration(int64(t.cfg.Base) * int64(attempt))
		if d > t.cfg.Cap {
			d = t.cfg.Cap
		}
		if t.jitterFactor > 0 {
			d += time.Duration(float64(d) * t.jitterFactor * rand.Float64())
			if d > t.cfg.Cap {
				d = t.cfg.Cap
			}
		}
		return d
	})
}

// ---------------------------------------------------------------------------
// Retry Budget
// ---------------------------------------------------------------------------

// BudgetConfig extends [Config] with a hard total-time ceiling.
type BudgetConfig struct {
	Config

	// TotalBudget is the maximum elapsed time across all retry attempts. Once
	// the budget is exhausted no further retries are issued even if MaxAttempts
	// has not been reached. Zero is treated as 10 s.
	TotalBudget time.Duration
}

// WithRetryBudget returns a [relay.Option] that retries using decorrelated
// jitter but stops as soon as the total elapsed wall time meets or exceeds
// TotalBudget. This gives a hard latency guarantee: the caller will wait at
// most ~TotalBudget regardless of how many retries were configured.
//
// Pair with [relay.WithDisableRetry] to avoid double-retrying.
func WithRetryBudget(cfg BudgetConfig) relay.Option {
	cfg.Config.applyDefaults()
	if cfg.TotalBudget <= 0 {
		cfg.TotalBudget = 10 * time.Second
	}
	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &budgetTransport{
			baseTransport: baseTransport{next: next, cfg: cfg.Config},
			totalBudget:   cfg.TotalBudget,
		}
	})
}

type budgetTransport struct {
	baseTransport
	totalBudget time.Duration
}

func (t *budgetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := prepareBody(req); err != nil {
		return nil, err
	}

	start := time.Now()
	prevSleep := t.cfg.Base
	var lastErr error

	for attempt := 0; attempt < t.cfg.MaxAttempts; attempt++ {
		if time.Since(start) >= t.totalBudget {
			break
		}

		if attempt > 0 {
			// Decorrelated jitter capped to remaining budget.
			upper := prevSleep * 3
			if upper > t.cfg.Cap {
				upper = t.cfg.Cap
			}
			lo := int64(t.cfg.Base)
			hi := int64(upper)
			if hi <= lo {
				hi = lo + 1
			}
			d := time.Duration(lo + rand.Int64N(hi-lo)) //nolint:nolintlint
			prevSleep = d

			remaining := t.totalBudget - time.Since(start)
			if d > remaining {
				d = remaining
			}
			if d <= 0 {
				break
			}

			if err := restoreBody(req); err != nil {
				return nil, err
			}
			if err := pause(req.Context(), d); err != nil {
				return nil, err
			}
		}

		resp, err := t.next.RoundTrip(req)

		if !t.isRetryable(resp, err) {
			return resp, err
		}
		if attempt == t.cfg.MaxAttempts-1 {
			if resp != nil {
				return resp, nil
			}
			if err != nil {
				return nil, err
			}
		}

		if t.cfg.OnRetry != nil {
			t.cfg.OnRetry(attempt+1, resp, err)
		}
		if resp != nil {
			// Drain up to 4 KiB so the underlying TCP connection can be reused.
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
		}
		lastErr = err
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, relay.ErrMaxRetriesReached
}
