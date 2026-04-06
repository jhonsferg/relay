package otel_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/jhonsferg/relay"
	relayotel "github.com/jhonsferg/relay/ext/otel"
)

func newTestServer(status int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
}

func TestWithTracing_SpanCreated(t *testing.T) {
	srv := newTestServer(http.StatusOK)
	defer srv.Close()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	tracer := tp.Tracer("test")

	c := relay.New(relayotel.WithTracing(tracer))

	_, _ = c.Execute(c.Get(srv.URL))

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span, got none")
	}
	span := spans[0]
	if span.Name != "HTTP GET" {
		t.Errorf("span name: got %q, want %q", span.Name, "HTTP GET")
	}

	// Verify http.method attribute is set.
	found := false
	for _, a := range span.Attributes {
		if a.Key == semconv.HTTPRequestMethodKey && a.Value.AsString() == "GET" {
			found = true
		}
	}
	if !found {
		t.Error("span missing http.method=GET attribute")
	}
}

func TestWithTracing_NilUsesGlobal(t *testing.T) {
	// nil tracer should not panic — falls back to global noop provider.
	srv := newTestServer(http.StatusOK)
	defer srv.Close()

	c := relay.New(relayotel.WithTracing(nil))
	resp, err := c.Execute(c.Get(srv.URL))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
}

func TestWithMetrics_Recorded(t *testing.T) {
	srv := newTestServer(http.StatusCreated)
	defer srv.Close()

	rdr := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	meter := mp.Meter("test")

	c := relay.New(relayotel.WithMetrics(meter))
	_, _ = c.Execute(c.Get(srv.URL))

	var rm metricdata.ResourceMetrics
	if err := rdr.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}

	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "http.client.requests.total" {
				found = true
			}
		}
	}
	if !found {
		t.Error("metric http.client.requests.total not recorded")
	}
}

func TestWithMetrics_NilUsesGlobal(t *testing.T) {
	srv := newTestServer(http.StatusOK)
	defer srv.Close()

	c := relay.New(relayotel.WithMetrics(nil))
	resp, err := c.Execute(c.Get(srv.URL))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
}

func TestWithOtel_Combined(t *testing.T) {
	srv := newTestServer(http.StatusOK)
	defer srv.Close()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	tracer := tp.Tracer("test")

	rdr := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(rdr))
	meter := mp.Meter("test")

	c := relay.New(relayotel.WithOtel(tracer, meter))
	_, _ = c.Execute(c.Get(srv.URL))

	if len(exp.GetSpans()) == 0 {
		t.Error("expected spans, got none")
	}
	var rm metricdata.ResourceMetrics
	if err := rdr.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect metrics: %v", err)
	}
	if len(rm.ScopeMetrics) == 0 {
		t.Error("expected metrics, got none")
	}
}

func TestWithTracing_ErrorSpan(t *testing.T) {
	// Use an unreachable address to trigger a transport error.
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	tracer := tp.Tracer("test")

	c := relay.New(
		relayotel.WithTracing(tracer),
		relay.WithRetry(nil),
	)
	_, _ = c.Execute(c.Get("http://127.0.0.1:1")) // port 1 is always refused

	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected error span, got none")
	}
}
