package tracing_test

import (
	"net/http"
	"testing"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/jhonsferg/relay"
	relaytracing "github.com/jhonsferg/relay/ext/tracing"
	"github.com/jhonsferg/relay/testutil"
)

// BenchmarkTracing_RoundTrip measures the per-request overhead of the
// tracing transport middleware: starting a client span, injecting the W3C
// TraceContext propagator into request headers, and recording response
// attributes/status, end to end through a relay.Client.
//
// A noop TracerProvider is used (as in tracing_test.go) so the benchmark
// isolates the middleware's own attribute-building/propagation cost rather
// than a particular SDK exporter's cost.
func BenchmarkTracing_RoundTrip(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	tp := noop.NewTracerProvider()
	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relaytracing.WithTracing(tp, prop),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: `{"ok":true}`})

		resp, err := client.Execute(client.Get("/bench"))
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("unexpected status %d", resp.StatusCode)
		}
		relay.PutResponse(resp)
	}
}
