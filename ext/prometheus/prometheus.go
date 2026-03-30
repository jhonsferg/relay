// Package prometheus integrates Prometheus metrics into the relay HTTP client.
// It registers and records request count, duration, and active-request gauges.
//
// Usage:
//
//	import (
//	    "github.com/jhonsferg/relay"
//	    relayprom "github.com/jhonsferg/relay/ext/prometheus"
//	    "github.com/prometheus/client_golang/prometheus"
//	)
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relayprom.WithPrometheus(nil, "myapp"), // nil = DefaultRegisterer
//	)
package prometheus

import (
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/jhonsferg/relay"
)

// WithPrometheus returns a [relay.Option] that records Prometheus metrics for
// every outgoing request. If registry is nil, prometheus.DefaultRegisterer is
// used. namespace is prepended to each metric name.
//
// The following metrics are registered:
//   - {namespace}_http_client_requests_total (counter, labels: method, host, status_code)
//   - {namespace}_http_client_request_duration_seconds (histogram, labels: method, host)
//   - {namespace}_http_client_active_requests (gauge, labels: method, host)
func WithPrometheus(registry prometheus.Registerer, namespace string) relay.Option {
	if registry == nil {
		registry = prometheus.DefaultRegisterer
	}
	m := newMetrics(registry, namespace)
	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &prometheusTransport{base: next, metrics: m}
	})
}

// metrics holds the registered Prometheus instruments.
type metrics struct {
	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	activeRequests  *prometheus.GaugeVec
}

func newMetrics(registry prometheus.Registerer, namespace string) *metrics {
	m := &metrics{
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "http_client_requests_total",
			Help:      "Total number of HTTP client requests, partitioned by method, host, and status code.",
		}, []string{"method", "host", "status_code"}),

		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "http_client_request_duration_seconds",
			Help:      "Duration of HTTP client requests in seconds, partitioned by method and host.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"method", "host"}),

		activeRequests: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "http_client_active_requests",
			Help:      "Number of HTTP client requests currently in-flight, partitioned by method and host.",
		}, []string{"method", "host"}),
	}

	// Register metrics, ignoring already-registered errors for hot-reload
	// scenarios (e.g. tests that re-create the client multiple times).
	for _, col := range []prometheus.Collector{m.requestsTotal, m.requestDuration, m.activeRequests} {
		if err := registry.Register(col); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				continue
			}
		}
	}
	return m
}

// prometheusTransport wraps an http.RoundTripper and records Prometheus metrics.
type prometheusTransport struct {
	base    http.RoundTripper
	metrics *metrics
}

func (t *prometheusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	method := req.Method

	t.metrics.activeRequests.WithLabelValues(method, host).Inc()
	timer := prometheus.NewTimer(prometheus.ObserverFunc(func(duration float64) {
		t.metrics.requestDuration.WithLabelValues(method, host).Observe(duration)
	}))

	resp, err := t.base.RoundTrip(req)

	timer.ObserveDuration()
	t.metrics.activeRequests.WithLabelValues(method, host).Dec()

	statusCode := "error"
	if err == nil && resp != nil {
		statusCode = strconv.Itoa(resp.StatusCode)
	}
	t.metrics.requestsTotal.WithLabelValues(method, host, statusCode).Inc()

	return resp, err
}
