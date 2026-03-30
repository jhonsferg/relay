// Package main demonstrates relay's OpenTelemetry extension modules:
// distributed tracing via github.com/jhonsferg/relay/ext/tracing and
// metrics via github.com/jhonsferg/relay/ext/metrics.
//
// To wire in real exporters (Jaeger, OTLP, stdout) add the SDK to go.mod:
//
//	go get go.opentelemetry.io/otel/sdk/trace
//	go get go.opentelemetry.io/otel/exporters/stdout/stdouttrace
//
// and replace the noop providers below with real ones.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	relay "github.com/jhonsferg/relay"
	relaymetrics "github.com/jhonsferg/relay/ext/metrics"
	relaytracing "github.com/jhonsferg/relay/ext/tracing"
)

func main() {
	// ---------------------------------------------------------------------------
	// Tracer provider setup.
	// Passing nil to relaytracing.WithTracing uses otel.GetTracerProvider(),
	// which is a safe no-op unless you've registered a real SDK provider.
	// ---------------------------------------------------------------------------
	var tp trace.TracerProvider = nil // replace with sdktrace.NewTracerProvider(...)

	// ---------------------------------------------------------------------------
	// Meter provider setup. Using the noop provider for compilation without SDK.
	// ---------------------------------------------------------------------------
	mp := noop.NewMeterProvider()

	// ---------------------------------------------------------------------------
	// Propagator: inject and extract W3C TraceContext + Baggage headers.
	// ---------------------------------------------------------------------------
	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(prop)

	// ---------------------------------------------------------------------------
	// Test server: echoes the traceparent header so we can verify injection.
	// ---------------------------------------------------------------------------
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent := r.Header.Get("Traceparent")
		if traceparent == "" {
			traceparent = "(not present)"
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"traceparent":%q,"path":%q}`, traceparent, r.URL.Path)
	}))
	defer srv.Close()

	// ---------------------------------------------------------------------------
	// Build the relay client with tracing and metrics enabled via ext modules.
	// ---------------------------------------------------------------------------
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relaytracing.WithTracing(tp, prop),
		relaymetrics.WithOTelMetrics(mp),
		relay.WithTimeout(10*time.Second),
		relay.WithDefaultHeaders(map[string]string{
			"User-Agent": "relay-otel-example/1.0",
		}),
	)

	// ---------------------------------------------------------------------------
	// Create a root span. relay's tracing middleware creates child spans.
	// ---------------------------------------------------------------------------
	tracer := otel.GetTracerProvider().Tracer("relay-otel-example")
	ctx, rootSpan := tracer.Start(context.Background(), "example-operation")
	defer rootSpan.End()

	paths := []string{"/users/1", "/orders/99", "/health"}
	for _, path := range paths {
		resp, err := client.Execute(
			client.Get(path).WithContext(ctx),
		)
		if err != nil {
			log.Fatalf("GET %s failed: %v", path, err)
		}
		fmt.Printf("GET %-15s → %d: %s\n", path, resp.StatusCode, resp.String())
	}

	fmt.Println("\n--- batch with tracing ---")
	batchReqs := []*relay.Request{
		client.Get("/a").WithContext(ctx),
		client.Get("/b").WithContext(ctx),
		client.Get("/c").WithContext(ctx),
	}
	results := client.ExecuteBatch(ctx, batchReqs, 3)
	for _, r := range results {
		if r.Err != nil {
			log.Printf("batch item %d error: %v", r.Index, r.Err)
			continue
		}
		fmt.Printf("  batch[%d] → %d\n", r.Index, r.Response.StatusCode)
	}

	fmt.Println("\nDone. With a real SDK exporter, spans and metrics would appear above.")
}
