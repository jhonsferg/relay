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
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relaytracing.WithTracing(nil, nil), // nil = use global provider/propagator
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

const instrumentationName = "github.com/jhonsferg/relay"

// WithTracing returns a [relay.Option] that adds OpenTelemetry client spans to
// every outgoing request. Pass nil for tp to use the global TracerProvider;
// pass nil for prop to use the global TextMapPropagator.
func WithTracing(tp trace.TracerProvider, prop propagation.TextMapPropagator) relay.Option {
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	if prop == nil {
		prop = otel.GetTextMapPropagator()
	}
	tracer := tp.Tracer(instrumentationName)
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
		startAttrs = append(startAttrs, attribute.String("net.peer.port", port))
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
