// Package prometheus integrates Prometheus metrics into the relay HTTP client.
// It registers and records request count, duration, body sizes, and in-flight gauges.
//
// Usage:
//
// import (
//
//	"github.com/jhonsferg/relay"
//	relayprom "github.com/jhonsferg/relay/ext/prometheus"
//	"github.com/prometheus/client_golang/prometheus"
//
// )
//
// client := relay.New(
//
//	relay.WithBaseURL("https://api.example.com"),
//	relayprom.WithPrometheus(nil, "myapp"), // nil = DefaultRegisterer
//	relayprom.WithPrometheus(nil, "myapp",
//	    relayprom.WithPrometheusHistograms(prometheus.DefBuckets),
//	    relayprom.WithPrometheusLabels("method", "host", "status_code"),
//	),
//
// )
package prometheus

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/jhonsferg/relay"
)

// PrometheusConfig holds optional configuration for histogram metrics.
type PrometheusConfig struct {
	durationBuckets []float64
	labels          []string
}

// defaultEnabledLabels is the label set used when WithPrometheusLabels is not called.
var defaultEnabledLabels = []string{"method", "host", "status_code"}

// WithPrometheusHistograms configures custom bucket boundaries for the request
// duration histograms. If nil, prometheus.DefBuckets is used.
func WithPrometheusHistograms(durationBuckets []float64) func(*PrometheusConfig) {
	return func(cfg *PrometheusConfig) {
		cfg.durationBuckets = durationBuckets
	}
}

// WithPrometheusLabels configures which labels are attached to histogram and
// gauge metrics. Available labels: "method", "host", "status_code", "path".
// When not called, the default set is {"method", "host", "status_code"}.
func WithPrometheusLabels(labels ...string) func(*PrometheusConfig) {
	return func(cfg *PrometheusConfig) {
		cfg.labels = labels
	}
}

// WithPrometheus returns a [relay.Option] that records Prometheus metrics for
// every outgoing request. If registry is nil, prometheus.DefaultRegisterer is
// used. namespace is prepended to each metric name.
//
// The following metrics are registered:
//   - {namespace}_http_client_requests_total (counter, labels: method, host, status_code)
//   - {namespace}_http_client_request_duration_seconds (histogram, labels: method, host)
//   - {namespace}_http_client_active_requests (gauge, labels: method, host)
//   - {namespace}_request_duration_seconds (histogram, labels: method, host, status_code)
//   - {namespace}_request_body_bytes (histogram, labels: method, host)
//   - {namespace}_response_body_bytes (histogram, labels: method, host, status_code)
//   - {namespace}_requests_in_flight (gauge, labels: host)
func WithPrometheus(registry prometheus.Registerer, namespace string, opts ...func(*PrometheusConfig)) relay.Option {
	if registry == nil {
		registry = prometheus.DefaultRegisterer
	}
	cfg := PrometheusConfig{labels: defaultEnabledLabels}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.durationBuckets == nil {
		cfg.durationBuckets = prometheus.DefBuckets
	}
	m := newMetrics(registry, namespace, cfg)
	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &prometheusTransport{base: next, metrics: m}
	})
}

// metrics holds the registered Prometheus instruments.
type metrics struct {
	// Legacy metrics kept for backwards compatibility.
	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	activeRequests  *prometheus.GaugeVec

	// Histogram and gauge metrics with richer label sets.
	reqDurationHist *prometheus.HistogramVec
	reqBodyHist     *prometheus.HistogramVec
	respBodyHist    *prometheus.HistogramVec
	inFlightGauge   *prometheus.GaugeVec

	// Resolved label names per metric (determined at construction time).
	reqDurationLabels []string
	reqBodyLabels     []string
	respBodyLabels    []string
	inFlightLabels    []string
}

// bodySizeBuckets covers HTTP body sizes from 64 B to ~64 MB.
var bodySizeBuckets = prometheus.ExponentialBuckets(64, 4, 10)

// selectLabels returns the elements of supported that are present in enabled,
// preserving the order of supported. Falls back to {"host"} when the result
// would be empty to avoid registering a metric with no labels at all.
func selectLabels(supported, enabled []string) []string {
	set := make(map[string]bool, len(enabled))
	for _, l := range enabled {
		set[l] = true
	}
	var result []string
	for _, l := range supported {
		if set[l] {
			result = append(result, l)
		}
	}
	if len(result) == 0 {
		return []string{"host"}
	}
	return result
}

func newMetrics(registry prometheus.Registerer, namespace string, cfg PrometheusConfig) *metrics {
	reqDurationLabels := selectLabels([]string{"method", "host", "status_code", "path"}, cfg.labels)
	reqBodyLabels := selectLabels([]string{"method", "host", "path"}, cfg.labels)
	respBodyLabels := selectLabels([]string{"method", "host", "status_code", "path"}, cfg.labels)
	inFlightLabels := selectLabels([]string{"host", "method", "path"}, cfg.labels)

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

		reqDurationHist: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_duration_seconds",
			Help:      "Duration of HTTP requests in seconds, with configurable labels.",
			Buckets:   cfg.durationBuckets,
		}, reqDurationLabels),

		reqBodyHist: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_body_bytes",
			Help:      "Size of HTTP request bodies in bytes.",
			Buckets:   bodySizeBuckets,
		}, reqBodyLabels),

		respBodyHist: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "response_body_bytes",
			Help:      "Size of HTTP response bodies in bytes.",
			Buckets:   bodySizeBuckets,
		}, respBodyLabels),

		inFlightGauge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "requests_in_flight",
			Help:      "Number of HTTP requests currently in-flight.",
		}, inFlightLabels),

		reqDurationLabels: reqDurationLabels,
		reqBodyLabels:     reqBodyLabels,
		respBodyLabels:    respBodyLabels,
		inFlightLabels:    inFlightLabels,
	}

	// Register metrics, ignoring already-registered errors for hot-reload
	// scenarios (e.g. tests that re-create the client multiple times).
	for _, col := range []prometheus.Collector{
		m.requestsTotal, m.requestDuration, m.activeRequests,
		m.reqDurationHist, m.reqBodyHist, m.respBodyHist, m.inFlightGauge,
	} {
		if err := registry.Register(col); err != nil {
			if !errors.As(err, new(prometheus.AlreadyRegisteredError)) {
				continue
			}
		}
	}
	return m
}

// labelValues constructs a prometheus.Labels map for the given label names,
// filling in the appropriate value for each recognised label key.
func labelValues(labels []string, method, host, statusCode, path string) prometheus.Labels {
	lv := make(prometheus.Labels, len(labels))
	for _, l := range labels {
		switch l {
		case "method":
			lv[l] = method
		case "host":
			lv[l] = host
		case "status_code":
			lv[l] = statusCode
		case "path":
			lv[l] = path
		}
	}
	return lv
}

// prometheusTransport wraps an http.RoundTripper and records Prometheus metrics.
type prometheusTransport struct {
	base    http.RoundTripper
	metrics *metrics
}

func (t *prometheusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Hostname()
	method := req.Method
	path := req.URL.Path

	t.metrics.activeRequests.WithLabelValues(method, host).Inc()
	t.metrics.inFlightGauge.With(labelValues(t.metrics.inFlightLabels, method, host, "", path)).Inc()

	if req.ContentLength >= 0 {
		t.metrics.reqBodyHist.With(
			labelValues(t.metrics.reqBodyLabels, method, host, "", path),
		).Observe(float64(req.ContentLength))
	}

	start := time.Now()
	resp, err := t.base.RoundTrip(req)
	elapsed := time.Since(start).Seconds()

	t.metrics.requestDuration.WithLabelValues(method, host).Observe(elapsed)
	t.metrics.activeRequests.WithLabelValues(method, host).Dec()
	t.metrics.inFlightGauge.With(labelValues(t.metrics.inFlightLabels, method, host, "", path)).Dec()

	statusCode := "error"
	if err == nil && resp != nil {
		statusCode = strconv.Itoa(resp.StatusCode)
		if resp.ContentLength >= 0 {
			t.metrics.respBodyHist.With(
				labelValues(t.metrics.respBodyLabels, method, host, statusCode, path),
			).Observe(float64(resp.ContentLength))
		}
	}

	t.metrics.reqDurationHist.With(
		labelValues(t.metrics.reqDurationLabels, method, host, statusCode, path),
	).Observe(elapsed)
	t.metrics.requestsTotal.WithLabelValues(method, host, statusCode).Inc()

	return resp, err
}
