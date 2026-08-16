// Package otel provides OpenTelemetry tracing and metrics integration for the
// relay HTTP client. It instruments every outgoing request with a client span
// and records request duration and count metrics.
//
// Usage:
//
//	import (
//	    "go.opentelemetry.io/otel/metric"
//	    "go.opentelemetry.io/otel/trace"
//	    "github.com/jhonsferg/relay"
//	    relayotel "github.com/jhonsferg/relay/ext/otel"
//	)
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relayotel.WithOtel(tracer, meter),
//	)
//
// Pass nil for either argument to use the global provider.
//
// # Tracing
//
// Each request creates a span named "HTTP {METHOD}" with the following
// semconv v1.26 attributes:
//   - http.request.method          - request method (e.g. GET)
//   - url.full                     - full request URL, credentials redacted
//   - http.response.status_code    - response status code
//   - http.response_content_length - response body size (when Content-Length is set)
//
// The span's trace context is also injected into the outgoing request headers
// via the global [propagation.TextMapPropagator] (W3C tracecontext by
// default), so downstream services can continue the distributed trace -
// the same propagation github.com/jhonsferg/relay/ext/tracing provides,
// plus the metrics below, in one option.
//
// Transport errors are recorded on the span and the span status is set to
// [codes.Error].
//
// # Metrics
//
// The following instruments are recorded:
//   - http.client.request.duration (histogram, milliseconds) - labelled by http.method
//   - http.client.requests.total (counter) - labelled by http.method and http.status_code
package otel

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	gotel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/jhonsferg/relay"
)

// WithTracing returns a [relay.Option] that creates an OpenTelemetry client
// span for every outgoing request and injects the current trace context into
// the outgoing request headers using the global [propagation.TextMapPropagator]
// (via [gotel.GetTextMapPropagator]), so downstream services can continue the
// distributed trace. Pass nil to use the global TracerProvider.
//
// Span name: "HTTP {METHOD}"
// Attributes: http.method, http.url, http.status_code,
// http.response_content_length (when available).
// Transport errors are recorded via [trace.Span.RecordError].
func WithTracing(tracer trace.Tracer) relay.Option {
	if tracer == nil {
		tracer = gotel.GetTracerProvider().Tracer("github.com/jhonsferg/relay/ext/otel")
	}
	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &tracingTransport{base: next, tracer: tracer, propagator: gotel.GetTextMapPropagator()}
	})
}

// WithMetrics returns a [relay.Option] that records OpenTelemetry metrics for
// every outgoing request. Pass nil to use the global MeterProvider.
//
// Instruments:
//   - http.client.request.duration (histogram, ms) - label: http.method
//   - http.client.requests.total (counter) - labels: http.method, http.status_code
func WithMetrics(meter metric.Meter) relay.Option {
	if meter == nil {
		meter = gotel.GetMeterProvider().Meter("github.com/jhonsferg/relay/ext/otel")
	}
	m, err := newInstruments(meter)
	if err != nil {
		// Instrument creation only fails when the meter is misconfigured; fall
		// back to noop so the client still works.
		m = &instruments{}
	}
	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &metricsTransport{base: next, inst: m}
	})
}

// WithOtel returns a [relay.Option] that enables both tracing and metrics.
// Pass nil for either argument to use the corresponding global provider.
func WithOtel(tracer trace.Tracer, meter metric.Meter) relay.Option {
	tracingOpt := WithTracing(tracer)
	metricsOpt := WithMetrics(meter)
	return func(cfg *relay.Config) {
		tracingOpt(cfg)
		metricsOpt(cfg)
	}
}

// tracingTransport is an http.RoundTripper that creates a span per request
// and injects the trace context into the outgoing request headers.
type tracingTransport struct {
	base       http.RoundTripper
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
}

func (t *tracingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	spanName := "HTTP " + req.Method

	ctx, span := t.tracer.Start(
		req.Context(),
		spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			semconv.HTTPRequestMethodKey.String(req.Method),
			// semconv v1.26: url.full is the stable key for the full request URL.
			// Per spec, credentials (userinfo) MUST be redacted.
			semconv.URLFullKey.String(redactURL(req.URL)),
		),
	)
	defer span.End()

	req = req.WithContext(ctx)
	if t.propagator != nil {
		t.propagator.Inject(ctx, propagation.HeaderCarrier(req.Header))
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	attrs := []attribute.KeyValue{
		semconv.HTTPResponseStatusCodeKey.Int(resp.StatusCode),
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

// instruments holds the OTel metric instruments.
type instruments struct {
	duration metric.Float64Histogram
	total    metric.Int64Counter
}

func newInstruments(m metric.Meter) (*instruments, error) {
	dur, err := m.Float64Histogram(
		"http.client.request.duration",
		metric.WithDescription("Duration of HTTP client requests in milliseconds."),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, fmt.Errorf("relay/ext/otel: create duration histogram: %w", err)
	}

	tot, err := m.Int64Counter(
		"http.client.requests.total",
		metric.WithDescription("Total number of HTTP client requests."),
	)
	if err != nil {
		return nil, fmt.Errorf("relay/ext/otel: create requests counter: %w", err)
	}

	return &instruments{duration: dur, total: tot}, nil
}

// metricsTransport is an http.RoundTripper that records OTel metrics per
// request.
type metricsTransport struct {
	base http.RoundTripper
	inst *instruments
}

func (t *metricsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	elapsed := float64(time.Since(start).Milliseconds())

	methodAttrs := []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(req.Method),
	}

	if t.inst.duration != nil {
		t.inst.duration.Record(req.Context(), elapsed, metric.WithAttributes(methodAttrs...))
	}

	if t.inst.total != nil {
		totalAttrs := methodAttrs
		// Per OTel semconv: http.response.status_code is an int attribute and
		// MUST only be set when the request completes with a valid HTTP response.
		// On transport-level errors (err != nil) the attribute is omitted.
		if err == nil && resp != nil {
			totalAttrs = append(totalAttrs, semconv.HTTPResponseStatusCodeKey.Int(resp.StatusCode)) //nolint:gocritic
		}
		t.inst.total.Add(req.Context(), 1, metric.WithAttributes(totalAttrs...))
	}

	return resp, err
}

// redactURL returns the URL string with the userinfo component removed.
// Per OTel semconv v1.26, url.full MUST NOT contain credentials
// (https://opentelemetry.io/schemas/1.26.0).
func redactURL(u *url.URL) string {
	if u.User == nil {
		return u.String()
	}
	redacted := *u
	redacted.User = nil
	return redacted.String()
}
