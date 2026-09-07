package otel_test

import (
	"net/http"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/jhonsferg/relay"
	relayotel "github.com/jhonsferg/relay/ext/otel"
)

// BenchmarkWithTracing measures the per-request overhead of span creation
// and trace-context propagation injection.
func BenchmarkWithTracing(b *testing.B) {
	srv := newTestServer(http.StatusOK)
	defer srv.Close()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	tracer := tp.Tracer("bench")

	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relayotel.WithTracing(tracer),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp, err := c.Execute(c.Get("/"))
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		_ = resp
	}
}

// BenchmarkWithMetrics measures the per-request overhead of recording the
// duration histogram and requests counter.
func BenchmarkWithMetrics(b *testing.B) {
	srv := newTestServer(http.StatusOK)
	defer srv.Close()

	mp := sdkmetric.NewMeterProvider()
	meter := mp.Meter("bench")

	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relayotel.WithMetrics(meter),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp, err := c.Execute(c.Get("/"))
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		_ = resp
	}
}
