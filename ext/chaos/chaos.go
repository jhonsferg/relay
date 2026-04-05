// Package chaos provides fault injection for relay clients.
// Use in tests and staging environments to validate resilience.
// NEVER use in production.
package chaos

import (
	"errors"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/jhonsferg/relay"
)

// Config defines the chaos injection parameters.
type Config struct {
	// ErrorRate is the probability [0.0, 1.0] of returning a synthetic error.
	ErrorRate float64
	// LatencyRate is the probability [0.0, 1.0] of injecting artificial latency.
	LatencyRate float64
	// Latency is the duration of injected latency when LatencyRate triggers.
	Latency time.Duration
	// Faults is a list of HTTP status codes to randomly inject.
	// Each entry has equal probability. If empty, no status faults are injected.
	Faults []int
	// FaultRate is the probability [0.0, 1.0] of injecting a fault status code.
	FaultRate float64
}

// ErrChaosInjected is returned when a synthetic error is injected.
var ErrChaosInjected = errors.New("chaos: injected error")

// Middleware returns a relay.Option that installs a chaos fault-injection
// transport middleware according to cfg. Faults are applied in this order:
// latency, then error, then fault status code.
func Middleware(cfg Config) relay.Option {
	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &chaosTransport{cfg: cfg, base: next}
	})
}

type chaosTransport struct {
	cfg  Config
	base http.RoundTripper
}

func (t *chaosTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Inject latency
	if t.cfg.LatencyRate > 0 && t.cfg.Latency > 0 && rand.Float64() < t.cfg.LatencyRate { //nolint:gosec
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(t.cfg.Latency):
		}
	}

	// Inject error
	if t.cfg.ErrorRate > 0 && rand.Float64() < t.cfg.ErrorRate { //nolint:gosec
		return nil, ErrChaosInjected
	}

	// Inject fault status
	if len(t.cfg.Faults) > 0 && t.cfg.FaultRate > 0 && rand.Float64() < t.cfg.FaultRate { //nolint:gosec
		statusCode := t.cfg.Faults[rand.IntN(len(t.cfg.Faults))] //nolint:gosec
		return &http.Response{
			StatusCode: statusCode,
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    req,
		}, nil
	}

	return t.base.RoundTrip(req)
}
