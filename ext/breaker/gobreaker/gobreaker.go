// Package gobreaker integrates github.com/sony/gobreaker as an alternative
// circuit breaker for the relay HTTP client.
//
// relay ships with its own three-state circuit breaker, but some projects
// prefer sony/gobreaker for its richer configuration options:
//   - Sliding-window failure counting (count-based or time-based)
//   - Per-state half-open request limits with success-ratio thresholds
//   - Custom ReadyToTrip and IsSuccessful predicates
//   - Named breakers for metrics and dashboard integration
//
// # Usage
//
//	import (
//	    "github.com/sony/gobreaker"
//	    "github.com/jhonsferg/relay"
//	    relaybreaker "github.com/jhonsferg/relay/ext/breaker/gobreaker"
//	)
//
//	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
//	    Name:        "my-api",
//	    MaxRequests: 3,
//	    Interval:    10 * time.Second,
//	    Timeout:     30 * time.Second,
//	    ReadyToTrip: func(counts gobreaker.Counts) bool {
//	        return counts.ConsecutiveFailures >= 5
//	    },
//	})
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relay.WithDisableCircuitBreaker(), // disable the built-in breaker
//	    relaybreaker.WithGoBreaker(cb),
//	)
//
// # Built-in vs gobreaker
//
// Use [relay.WithDisableCircuitBreaker] alongside this option to avoid double
// circuit-breaking. Alternatively, keep both if you want gobreaker's richer
// failure counting around the relay retrier and relay's built-in breaker as a
// fast-path guard.
//
// # Error mapping
//
// gobreaker returns [gobreaker.ErrOpenState] or [gobreaker.ErrTooManyRequests]
// when the breaker is open or in half-open saturation. Both are wrapped and
// returned from [relay.Client.Execute] as-is; you can unwrap them with
// [errors.Is] / [errors.As].
package gobreaker

import (
	"net/http"

	gb "github.com/sony/gobreaker"

	"github.com/jhonsferg/relay"
)

// WithGoBreaker returns a [relay.Option] that installs a sony/gobreaker circuit
// breaker as a transport middleware. Each HTTP call (after relay's retrier) is
// executed inside cb.Execute - failures recorded by the breaker are determined
// by the IsSuccessful setting in the provided Settings.
//
// By default (when IsSuccessful is nil) the breaker counts any error OR any
// HTTP 5xx response as a failure.
func WithGoBreaker(cb *gb.CircuitBreaker) relay.Option {
	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &gobreakerTransport{base: next, cb: cb}
	})
}

// NewCircuitBreaker is a convenience wrapper around [gb.NewCircuitBreaker] that
// injects a default IsSuccessful predicate treating 5xx responses as failures.
// You can override this by setting Settings.IsSuccessful before calling New.
func NewCircuitBreaker(settings gb.Settings) *gb.CircuitBreaker {
	if settings.IsSuccessful == nil {
		settings.IsSuccessful = func(err error) bool { return err == nil }
	}
	return gb.NewCircuitBreaker(settings)
}

// gobreakerTransport wraps each RoundTrip in a gobreaker Execute call.
type gobreakerTransport struct {
	base http.RoundTripper
	cb   *gb.CircuitBreaker
}

func (t *gobreakerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	result, err := t.cb.Execute(func() (any, error) {
		resp, err := t.base.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		// Treat 5xx responses as breaker failures by wrapping them in an error.
		// The caller still receives the *http.Response via the typed result.
		if resp.StatusCode >= 500 {
			return resp, &httpStatusError{resp: resp}
		}
		return resp, nil
	})

	if err != nil {
		// If the error is our own httpStatusError the breaker tripped on a 5xx -
		// unwrap and return the response + no error so relay can inspect it.
		if se, ok := err.(*httpStatusError); ok {
			return se.resp, nil
		}
		return nil, err
	}

	return result.(*http.Response), nil
}

// httpStatusError is an internal sentinel used to signal a 5xx response to
// gobreaker's failure counter without hiding the response from the caller.
type httpStatusError struct{ resp *http.Response }

func (e *httpStatusError) Error() string {
	return "http status error: " + e.resp.Status
}
