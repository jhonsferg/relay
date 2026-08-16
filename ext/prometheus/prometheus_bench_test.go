package prometheus_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhonsferg/relay"
	relayprom "github.com/jhonsferg/relay/ext/prometheus"
)

// BenchmarkWithPrometheus_DefaultLabels measures the per-request overhead of
// recording all seven metrics (counters, histograms, gauges) with the
// default label set.
func BenchmarkWithPrometheus_DefaultLabels(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	reg := newRegistry()
	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relayprom.WithPrometheus(reg, "bench"),
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

// BenchmarkWithPrometheus_AllLabels measures overhead with the full label
// set enabled (including the high-cardinality "path" label).
func BenchmarkWithPrometheus_AllLabels(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":1}`))
	}))
	defer srv.Close()

	reg := newRegistry()
	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relayprom.WithPrometheus(reg, "benchall",
			relayprom.WithPrometheusLabels("method", "host", "status_code", "path"),
		),
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
