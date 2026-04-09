// Package tracing integrates OpenTelemetry distributed tracing into the relay
// HTTP client. Each outgoing request creates a client span and injects the
// trace context (W3C TraceContext by default) into the request headers so
// downstream services can continue the distributed trace.
//
// Usage:
//
//	import (
//	    "github.com/jhonsferg/relay"
//	    relaytracing "github.com/jhonsferg/relay/ext/tracing"
//	)
//
//	// Default instrumentation name:
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relaytracing.WithTracing(nil, nil),
//	)
//
//	// Custom instrumentation name and version:
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relaytracing.WithTracing(nil, nil,
//	        relaytracing.WithInstrumentationName("my-service"),
//	        relaytracing.WithInstrumentationVersion("1.0.0"),
//	    ),
//	)
package tracing

import (
	"net/http"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/jhonsferg/relay"
)

const defaultInstrumentationName = "github.com/jhonsferg/relay"

// Option configures the tracing middleware.
type Option func(*tracingConfig)

type tracingConfig struct {
	instrumentationName    string
	instrumentationVersion string
}

// WithInstrumentationName sets the OpenTelemetry instrumentation scope name
// used when creating the tracer. If empty, defaults to
// "github.com/jhonsferg/relay".
func WithInstrumentationName(name string) Option {
	return func(c *tracingConfig) {
		if name != "" {
			c.instrumentationName = name
		}
	}
}

// WithInstrumentationVersion sets the instrumentation scope version string
// attached to every span produced by this middleware (e.g. "1.0.0").
func WithInstrumentationVersion(version string) Option {
	return func(c *tracingConfig) {
		c.instrumentationVersion = version
	}
}

// WithTracing returns a [relay.Option] that adds OpenTelemetry client spans to
// every outgoing request. Pass nil for tp to use the global TracerProvider;
// pass nil for prop to use the global TextMapPropagator.
//
// Use the functional [Option] helpers to customise the instrumentation scope:
//
//	relaytracing.WithTracing(tp, prop,
//	    relaytracing.WithInstrumentationName("my-service"),
//	    relaytracing.WithInstrumentationVersion("2.0.0"),
//	)
func WithTracing(tp trace.TracerProvider, prop propagation.TextMapPropagator, opts ...Option) relay.Option {
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	if prop == nil {
		prop = otel.GetTextMapPropagator()
	}

	cfg := &tracingConfig{instrumentationName: defaultInstrumentationName}
	for _, o := range opts {
		o(cfg)
	}

	var tracerOpts []trace.TracerOption
	if cfg.instrumentationVersion != "" {
		tracerOpts = append(tracerOpts, trace.WithInstrumentationVersion(cfg.instrumentationVersion))
	}
	tracer := tp.Tracer(cfg.instrumentationName, tracerOpts...)

	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &tracingTransport{base: next, tracer: tracer, propagator: prop}
	})
}

// tracingTransport wraps an http.RoundTripper with OTel client span creation.
type tracingTransport struct {
	base       http.RoundTripper
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
}

// RoundTrip starts a client span, injects trace context, sends the request,
// and sets span status and attributes from the response.
func (t *tracingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// "METHOD host" avoids high-cardinality names from path variables.
	spanName := req.Method + " " + req.URL.Hostname()

	startAttrs := []attribute.KeyValue{
		attribute.String("http.method", req.Method),
		attribute.String("http.url", req.URL.String()),
		attribute.String("http.host", req.Host),
		attribute.String("http.target", req.URL.RequestURI()),
		attribute.String("net.peer.name", req.URL.Hostname()),
	}
	if port := req.URL.Port(); port != "" {
		if portInt, err := strconv.Atoi(port); err == nil {
			startAttrs = append(startAttrs, attribute.Int("net.peer.port", portInt))
		}
	}

	ctx, span := t.tracer.Start(
		req.Context(),
		spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(startAttrs...),
	)
	defer span.End()

	req = req.WithContext(ctx)
	t.propagator.Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	attrs := []attribute.KeyValue{
		attribute.Int("http.status_code", resp.StatusCode),
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if n, parseErr := strconv.ParseInt(cl, 10, 64); parseErr == nil {
			attrs = append(attrs, attribute.Int64("http.response_content_length", n))
		}
	}
	span.SetAttributes(attrs...)

	if resp.StatusCode >= 400 {
		span.SetStatus(codes.Error, resp.Status)
	} else {
		span.SetStatus(codes.Ok, "")
	}

	return resp, nil
}
