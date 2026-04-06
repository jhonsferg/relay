package relay

import (
	"net/http"
	"time"
)

// loggingTransport wraps an http.RoundTripper and logs every request/response
// cycle via the configured [Logger].
type loggingTransport struct {
	base   http.RoundTripper
	logger Logger
}

// RoundTrip logs the outbound request and the response status + latency.
func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()

	t.logger.Debug("relay: request",
		"method", req.Method,
		"url", req.URL.String(),
	)

	resp, err := t.base.RoundTrip(req)
	latency := time.Since(start)

	if err != nil {
		t.logger.Warn("relay: request failed",
			"method", req.Method,
			"url", req.URL.String(),
			"latency_ms", latency.Milliseconds(),
			"error", err,
		)
		return nil, err
	}

	args := []any{
		"method", req.Method,
		"url", req.URL.String(),
		"status", resp.StatusCode,
		"latency_ms", latency.Milliseconds(),
	}
	if resp.StatusCode >= 400 {
		t.logger.Warn("relay: response", args...)
	} else {
		t.logger.Debug("relay: response", args...)
	}

	return resp, nil
}

// WithRequestLogger adds a transport-level middleware that logs every
// request/response cycle using the provided [Logger].
//
// Requests are logged at Debug level with method and URL. Responses are logged
// at Debug for 2xx/3xx and at Warn for 4xx/5xx, including status code and
// round-trip latency in milliseconds.
//
// Pair with [SlogAdapter] or [NewDefaultLogger] to integrate with your
// application's existing logging setup:
//
// client := relay.New(
//
//	relay.WithRequestLogger(relay.SlogAdapter(slog.Default())),
//
// )
func WithRequestLogger(logger Logger) Option {
	if logger == nil {
		return func(*Config) {}
	}
	return WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &loggingTransport{base: next, logger: logger}
	})
}
