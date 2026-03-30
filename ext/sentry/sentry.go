// Package sentry integrates Sentry error monitoring into the relay HTTP client.
// It captures transport-level errors, HTTP 5xx responses, and optionally HTTP
// 4xx responses as Sentry events. It also records all completed requests as
// Sentry breadcrumbs for trace context.
//
// Usage:
//
//	import (
//	    sentrygo "github.com/getsentry/sentry-go"
//	    "github.com/jhonsferg/relay"
//	    relaysentry "github.com/jhonsferg/relay/ext/sentry"
//	)
//
//	// Initialise Sentry once at startup.
//	sentrygo.Init(sentrygo.ClientOptions{Dsn: "https://...@sentry.io/..."})
//
//	// Each client gets its own Hub clone for isolated scope management.
//	hub := sentrygo.CurrentHub().Clone()
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relaysentry.WithSentry(hub),
//	)
//
// # Per-request scoping
//
// Each RoundTrip clones the supplied Hub so that request-specific scope
// mutations (request URL, method, status) never leak into the parent Hub or
// affect concurrent requests.
//
// # Functional options
//
// The behavior can be tuned with [Option] functions:
//
//   - [WithCaptureTransportErrors] — capture network/timeout errors (default: true)
//   - [WithCaptureServerErrors]    — capture 5xx responses as events (default: true)
//   - [WithCaptureClientErrors]    — capture 4xx responses as events (default: false)
//   - [WithBreadcrumbs]            — add breadcrumb for every request (default: true)
package sentry

import (
	"fmt"
	"net/http"

	sentrygo "github.com/getsentry/sentry-go"

	"github.com/jhonsferg/relay"
)

// Option configures the Sentry transport.
type Option func(*sentryConfig)

type sentryConfig struct {
	captureTransportErrors bool
	captureServerErrors    bool
	captureClientErrors    bool
	addBreadcrumbs         bool
}

func defaultConfig() sentryConfig {
	return sentryConfig{
		captureTransportErrors: true,
		captureServerErrors:    true,
		captureClientErrors:    false,
		addBreadcrumbs:         true,
	}
}

// WithCaptureTransportErrors controls whether network/timeout errors are sent
// to Sentry as exceptions. Enabled by default.
func WithCaptureTransportErrors(enabled bool) Option {
	return func(c *sentryConfig) { c.captureTransportErrors = enabled }
}

// WithCaptureServerErrors controls whether HTTP 5xx responses are sent to
// Sentry as events. Enabled by default.
func WithCaptureServerErrors(enabled bool) Option {
	return func(c *sentryConfig) { c.captureServerErrors = enabled }
}

// WithCaptureClientErrors controls whether HTTP 4xx responses are sent to
// Sentry as events. Disabled by default.
func WithCaptureClientErrors(enabled bool) Option {
	return func(c *sentryConfig) { c.captureClientErrors = enabled }
}

// WithBreadcrumbs controls whether a breadcrumb is added for every completed
// request (success or HTTP error). Enabled by default.
func WithBreadcrumbs(enabled bool) Option {
	return func(c *sentryConfig) { c.addBreadcrumbs = enabled }
}

// WithSentry returns a [relay.Option] that attaches Sentry error reporting to
// the relay transport chain. hub is cloned for each request to keep scopes
// isolated. Pass [sentrygo.CurrentHub] or a pre-configured Hub.
//
// Additional behavior can be tuned with [Option] values.
func WithSentry(hub *sentrygo.Hub, opts ...Option) relay.Option {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &sentryTransport{base: next, hub: hub, cfg: cfg}
	})
}

type sentryTransport struct {
	base http.RoundTripper
	hub  *sentrygo.Hub
	cfg  sentryConfig
}

func (t *sentryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone so this request's scope does not bleed into the parent Hub.
	localHub := t.hub.Clone()
	localHub.ConfigureScope(func(scope *sentrygo.Scope) {
		scope.SetRequest(req)
		scope.SetTag("http.method", req.Method)
		scope.SetTag("http.host", req.URL.Host)
	})

	resp, err := t.base.RoundTrip(req)

	if err != nil {
		if t.cfg.captureTransportErrors {
			localHub.CaptureException(err)
		}
		return nil, err
	}

	switch {
	case resp.StatusCode >= 500 && t.cfg.captureServerErrors:
		localHub.CaptureEvent(buildEvent(req, resp, sentrygo.LevelError))
	case resp.StatusCode >= 400 && t.cfg.captureClientErrors:
		localHub.CaptureEvent(buildEvent(req, resp, sentrygo.LevelWarning))
	}

	if t.cfg.addBreadcrumbs {
		localHub.AddBreadcrumb(buildBreadcrumb(req, resp), nil)
	}

	return resp, nil
}

// buildEvent creates a Sentry event from a request/response pair.
func buildEvent(req *http.Request, resp *http.Response, level sentrygo.Level) *sentrygo.Event {
	return &sentrygo.Event{
		Level:   level,
		Message: fmt.Sprintf("%s %s → %d %s", req.Method, req.URL.String(), resp.StatusCode, resp.Status),
		Tags: map[string]string{
			"http.method":      req.Method,
			"http.host":        req.URL.Host,
			"http.status_code": fmt.Sprintf("%d", resp.StatusCode),
		},
		Request: &sentrygo.Request{
			URL:         req.URL.String(),
			Method:      req.Method,
			QueryString: req.URL.RawQuery,
		},
	}
}

// buildBreadcrumb creates a breadcrumb for a completed HTTP request.
func buildBreadcrumb(req *http.Request, resp *http.Response) *sentrygo.Breadcrumb {
	level := sentrygo.LevelInfo
	if resp.StatusCode >= 500 {
		level = sentrygo.LevelError
	} else if resp.StatusCode >= 400 {
		level = sentrygo.LevelWarning
	}
	return &sentrygo.Breadcrumb{
		Type:     "http",
		Category: "http",
		Level:    level,
		Data: map[string]interface{}{
			"url":         req.URL.String(),
			"method":      req.Method,
			"status_code": resp.StatusCode,
			"reason":      resp.Status,
		},
	}
}
