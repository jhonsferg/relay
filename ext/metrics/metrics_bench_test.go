package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/metric/noop"

	"github.com/jhonsferg/relay"
	relaymetrics "github.com/jhonsferg/relay/ext/metrics"
)

// BenchmarkWithOTelMetrics_RoundTrip measures the per-request overhead of
// metricsTransport.RoundTrip - the hot path that records counters/histograms
// on every outgoing request - using a noop MeterProvider so the measurement
// isolates the middleware's own cost (attribute construction, timing) rather
// than a real metrics backend.
func BenchmarkWithOTelMetrics_RoundTrip(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relaymetrics.WithOTelMetrics(noop.NewMeterProvider()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		resp, err := c.Execute(c.Get("/"))
		if err != nil {
			b.Fatalf("Execute: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	}
}
