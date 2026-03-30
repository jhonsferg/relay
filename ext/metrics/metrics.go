// Package metrics integrates OpenTelemetry metrics instrumentation into the
// relay HTTP client. It records request count, duration, and active-request
// counters using the OTel HTTP semantic conventions.
//
// Usage:
//
//	import (
//	    "github.com/jhonsferg/relay"
//	    relaymetrics "github.com/jhonsferg/relay/ext/metrics"
//	)
//
//	// Default instrumentation name:
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relaymetrics.WithOTelMetrics(nil),
//	)
//
//	// Custom instrumentation name and version:
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relaymetrics.WithOTelMetrics(nil,
//	        relaymetrics.WithInstrumentationName("my-service"),
//	        relaymetrics.WithInstrumentationVersion("1.0.0"),
//	    ),
//	)
package metrics

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/jhonsferg/relay"
)

const defaultInstrumentationName = "github.com/jhonsferg/relay"

// Option configures the metrics middleware.
type Option func(*metricsConfig)

type metricsConfig struct {
	instrumentationName    string
	instrumentationVersion string
}

// WithInstrumentationName sets the OpenTelemetry instrumentation scope name
// used when creating the meter. If empty, defaults to
// "github.com/jhonsferg/relay".
func WithInstrumentationName(name string) Option {
	return func(c *metricsConfig) {
		if name != "" {
			c.instrumentationName = name
		}
	}
}

// WithInstrumentationVersion sets the instrumentation scope version string
// attached to every metric recorded by this middleware (e.g. "1.0.0").
func WithInstrumentationVersion(version string) Option {
	return func(c *metricsConfig) {
		c.instrumentationVersion = version
	}
}

// WithOTelMetrics returns a [relay.Option] that records OpenTelemetry metrics
// for every outgoing request. Pass nil to use the globally registered
// [metric.MeterProvider].
//
// The following instruments are created under the instrumentation scope
// (default "github.com/jhonsferg/relay"):
//   - http.client.request_count (counter)
//   - http.client.request_duration_ms (histogram, milliseconds)
//   - http.client.active_requests (up-down counter)
//
// Use the functional [Option] helpers to customise the instrumentation scope:
//
//	relaymetrics.WithOTelMetrics(mp,
//	    relaymetrics.WithInstrumentationName("my-service"),
//	    relaymetrics.WithInstrumentationVersion("2.0.0"),
//	)
func WithOTelMetrics(mp metric.MeterProvider, opts ...Option) relay.Option {
	if mp == nil {
		mp = otel.GetMeterProvider()
	}

	cfg := &metricsConfig{instrumentationName: defaultInstrumentationName}
	for _, o := range opts {
		o(cfg)
	}

	var meterOpts []metric.MeterOption
	if cfg.instrumentationVersion != "" {
		meterOpts = append(meterOpts, metric.WithInstrumentationVersion(cfg.instrumentationVersion))
	}

	m := newInstruments(mp.Meter(cfg.instrumentationName, meterOpts...))
	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &metricsTransport{base: next, instruments: m}
	})
}

// instruments holds the OTel metric instruments.
type instruments struct {
	requestCount    metric.Int64Counter
	requestDuration metric.Float64Histogram
	activeRequests  metric.Int64UpDownCounter
}

func newInstruments(meter metric.Meter) *instruments {
	requestCount, _ := meter.Int64Counter(
		"http.client.request_count",
		metric.WithDescription("Total number of outbound HTTP requests"),
		metric.WithUnit("{request}"),
	)
	requestDuration, _ := meter.Float64Histogram(
		"http.client.request_duration_ms",
		metric.WithDescription("Duration of outbound HTTP requests in milliseconds"),
		metric.WithUnit("ms"),
	)
	activeRequests, _ := meter.Int64UpDownCounter(
		"http.client.active_requests",
		metric.WithDescription("Number of in-flight outbound HTTP requests"),
		metric.WithUnit("{request}"),
	)
	return &instruments{
		requestCount:    requestCount,
		requestDuration: requestDuration,
		activeRequests:  activeRequests,
	}
}

// metricsTransport wraps an http.RoundTripper and records OTel metrics.
type metricsTransport struct {
	base        http.RoundTripper
	instruments *instruments
}

func (t *metricsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	method := req.Method
	host := req.URL.Hostname()

	t.instruments.activeRequests.Add(ctx, 1)
	start := time.Now()

	resp, err := t.base.RoundTrip(req)

	elapsed := time.Since(start)
	t.instruments.activeRequests.Add(ctx, -1)

	statusCode := 0
	if err == nil && resp != nil {
		statusCode = resp.StatusCode
	}
	t.record(ctx, method, host, statusCode, elapsed, err != nil)
	return resp, err
}

func (t *metricsTransport) record(ctx context.Context, method, host string, statusCode int, d time.Duration, failed bool) {
	attrs := metric.WithAttributes(
		attribute.String("http.method", method),
		attribute.String("net.peer.name", host),
		attribute.Int("http.status_code", statusCode),
		attribute.Bool("error", failed),
	)
	t.instruments.requestCount.Add(ctx, 1, attrs)
	t.instruments.requestDuration.Record(ctx, float64(d.Milliseconds()), attrs)
}
