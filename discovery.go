package relay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// Resolver selects the backend ("host:port") for a single request attempt.
//
// It is called once per attempt - including every retry - so an
// implementation that balances across instances naturally sends a retried
// request to a different backend. Implementations must be safe for
// concurrent use and should return quickly: Resolve sits on the hot path of
// every request, so it should serve from an in-memory view of the backends
// (for example one kept up to date by a service-discovery watch) rather than
// performing network I/O per call.
//
// [SRVResolver] implements Resolver. Any type with a matching Resolve method
// satisfies it, so service-discovery libraries can plug in without importing
// relay.
type Resolver interface {
	Resolve(ctx context.Context) (string, error)
}

// ResolverFunc adapts a plain function to [Resolver].
type ResolverFunc func(ctx context.Context) (string, error)

// Resolve implements [Resolver].
func (f ResolverFunc) Resolve(ctx context.Context) (string, error) { return f(ctx) }

// FailureReporter is optionally implemented by a [Resolver] that wants to
// learn about transport-level failures against a target it returned, so it
// can stop selecting that target before its source of truth (DNS TTL, a
// discovery watch, a health check) notices - passive outlier detection.
//
// ReportFailure is called with the exact target string Resolve returned and
// the transport error, for errors such as a refused or reset connection. It
// is never called for HTTP error statuses (the backend answered) nor for
// context cancellation or deadline expiry (the caller gave up, which says
// nothing about the backend). It must be safe for concurrent use and must not
// block.
type FailureReporter interface {
	ReportFailure(target string, err error)
}

// discoveryRoundTripper rewrites the request host to the target chosen by a
// Resolver before forwarding it, and reports transport failures back to the
// resolver when it implements FailureReporter.
type discoveryRoundTripper struct {
	next     http.RoundTripper
	resolver Resolver
	reporter FailureReporter // nil when resolver does not implement it
}

// RoundTrip implements http.RoundTripper.
func (d discoveryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	target, err := d.resolver.Resolve(req.Context())
	if err != nil {
		return nil, fmt.Errorf("relay: discovery resolve: %w", err)
	}
	reqCopy := req.Clone(req.Context())
	reqCopy.URL.Host = target
	reqCopy.Host = target

	resp, err := d.next.RoundTrip(reqCopy)
	if err != nil && d.reporter != nil && isBackendFailure(err) {
		d.reporter.ReportFailure(target, err)
	}
	return resp, err
}

// isBackendFailure reports whether a transport error says something about the
// backend itself. Cancellation and deadlines reflect the caller's intent (or
// budget), not the backend's health, so they are excluded.
func isBackendFailure(err error) bool {
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// DiscoveryMiddleware returns a transport middleware that resolves the
// backend for every request attempt through r and rewrites the request host
// accordingly. When r also implements [FailureReporter], transport failures
// against a resolved target are reported back to it.
//
// Most callers use [WithDiscovery]; the middleware form exists for composing
// with other transports explicitly.
func DiscoveryMiddleware(r Resolver) func(http.RoundTripper) http.RoundTripper {
	reporter, _ := r.(FailureReporter)
	return func(next http.RoundTripper) http.RoundTripper {
		return discoveryRoundTripper{next: next, resolver: r, reporter: reporter}
	}
}

// WithDiscovery routes every request attempt to the backend chosen by r,
// replacing the host of the base URL (the scheme and path are kept). Because
// the resolver runs per attempt, retries are re-resolved and - with a
// balancing resolver - land on another instance.
//
// Pair it with an idempotency key ([Request.WithIdempotencyKey] or
// [WithAutoIdempotencyKey]) when non-idempotent requests (POST, PATCH) must
// also be retried on another instance: relay never retries them otherwise.
//
// Example with a discovery library exposing a compatible resolver:
//
//	client := relay.New(
//	    relay.WithBaseURL("http://integrator"), // host replaced per attempt
//	    relay.WithDiscovery(balancer.For("integrator")),
//	)
func WithDiscovery(r Resolver) Option {
	return WithTransportMiddleware(DiscoveryMiddleware(r))
}
