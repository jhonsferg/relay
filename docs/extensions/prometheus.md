# Prometheus Metrics Extension

The `prometheus` extension exposes relay request metrics in the Prometheus text format. It integrates directly with `prometheus/client_golang`, registering counters, histograms, and gauges into a Prometheus registry that you expose via a standard `/metrics` HTTP endpoint.

Use this extension when you run a Prometheus scrape pipeline and do not need a full OpenTelemetry SDK. If you already have an OTel pipeline, consider the [metrics extension](metrics.md) instead, which can also export to Prometheus via the OTel Prometheus bridge.

## Installation

```bash
go get github.com/jhonsferg/relay/ext/prometheus
```

## Import

```go
import relayprom "github.com/jhonsferg/relay/ext/prometheus"
```

## Quick Start

```go
package main

import (
    "context"
    "log"
    "net/http"

    "github.com/jhonsferg/relay"
    relayprom "github.com/jhonsferg/relay/ext/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relayprom.WithPrometheus(),
    )
    if err != nil {
        log.Fatalf("relay.New: %v", err)
    }
    defer client.Close()

    // Expose metrics at /metrics.
    http.Handle("/metrics", promhttp.Handler())
    go func() {
        if err := http.ListenAndServe(":9090", nil); err != nil {
            log.Fatalf("metrics server: %v", err)
        }
    }()

    ctx := context.Background()
    resp, err := client.Get(ctx, "/health")
    if err != nil {
        log.Fatalf("GET /health: %v", err)
    }
    defer resp.Body.Close()
    log.Printf("status: %d", resp.StatusCode)
}
```

## API Reference

### `relayprom.WithPrometheus(opts...)`

```go
func WithPrometheus(opts ...PrometheusOption) relay.Option
```

Registers the relay Prometheus collectors and wraps the client transport with an instrumented layer. The function is idempotent per registry - calling it twice with the same registry returns an error from the second call.

### Options

| Option Function | Signature | Description |
|-----------------|-----------|-------------|
| `WithNamespace` | `WithNamespace(ns string)` | Prefix all metric names with `ns_`. Default: `""` (no prefix). |
| `WithSubsystem` | `WithSubsystem(sub string)` | Add a subsystem component between namespace and metric name. |
| `WithRegisterer` | `WithRegisterer(reg prometheus.Registerer)` | Use a custom registry instead of `prometheus.DefaultRegisterer`. |
| `WithGatherer` | `WithGatherer(g prometheus.Gatherer)` | Use a custom gatherer for `promhttp.HandlerFor`. |
| `WithDurationBuckets` | `WithDurationBuckets(buckets []float64)` | Override histogram bucket boundaries. |
| `WithConstLabels` | `WithConstLabels(labels prometheus.Labels)` | Attach fixed labels to all metrics from this client. |
| `WithObserveRequestSize` | `WithObserveRequestSize(bool)` | Enable the request body size histogram. Default: `false`. |
| `WithObserveResponseSize` | `WithObserveResponseSize(bool)` | Enable the response body size histogram. Default: `false`. |

## Metric Names and Labels

The extension registers the following collectors. All names assume no namespace prefix; set `WithNamespace("myapp")` to get `myapp_relay_requests_total`, etc.

### `relay_requests_total`

A counter vector counting completed requests.

- **Type:** `CounterVec`
- **Labels:** `method`, `host`, `status_code`

When a transport-level error occurs and no response is received, `status_code` is set to `"error"`.

### `relay_request_duration_seconds`

A histogram of request durations. The duration is measured from the first byte sent to the response body being fully read or closed.

- **Type:** `HistogramVec`
- **Labels:** `method`, `host`, `status_code`
- **Default buckets (seconds):** `.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10`

### `relay_active_requests`

A gauge tracking in-flight requests.

- **Type:** `GaugeVec`
- **Labels:** `method`, `host`

### `relay_request_size_bytes` _(optional)_

A histogram of request body sizes in bytes. Enable with `WithObserveRequestSize(true)`.

- **Type:** `HistogramVec`
- **Labels:** `method`, `host`

### `relay_response_size_bytes` _(optional)_

A histogram of response body sizes in bytes. Enable with `WithObserveResponseSize(true)`.

- **Type:** `HistogramVec`
- **Labels:** `method`, `host`, `status_code`

## Complete Example: /metrics Endpoint with Custom Registry

Using a custom registry keeps relay metrics isolated from default Go runtime metrics during testing, or when running multiple relay clients in the same process.

```go
package main

import (
    "context"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/jhonsferg/relay"
    relayprom "github.com/jhonsferg/relay/ext/prometheus"
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/collectors"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
    // Create a custom registry with Go runtime and process metrics.
    reg := prometheus.NewRegistry()
    reg.MustRegister(
        collectors.NewGoCollector(),
        collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
    )

    // Build the relay client, recording all metrics into the custom registry.
    client, err := relay.New(
        relay.WithBaseURL("https://jsonplaceholder.typicode.com"),
        relay.WithTimeout(10),
        relayprom.WithPrometheus(
            relayprom.WithNamespace("myapp"),
            relayprom.WithSubsystem("http_client"),
            relayprom.WithRegisterer(reg),
            relayprom.WithDurationBuckets([]float64{
                .005, .01, .025, .05, .1, .25, .5, 1, 2.5,
            }),
            relayprom.WithConstLabels(prometheus.Labels{
                "service": "user-service",
            }),
            relayprom.WithObserveResponseSize(true),
        ),
    )
    if err != nil {
        log.Fatalf("relay.New: %v", err)
    }
    defer client.Close()

    // Serve the custom registry on /metrics.
    mux := http.NewServeMux()
    mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
        EnableOpenMetrics: true,
    }))
    mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
        w.WriteHeader(http.StatusOK)
    })

    srv := &http.Server{
        Addr:         ":8080",
        Handler:      mux,
        ReadTimeout:  5 * time.Second,
        WriteTimeout: 10 * time.Second,
    }

    go func() {
        log.Printf("metrics server listening on :8080")
        if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Fatalf("metrics server: %v", err)
        }
    }()

    // Make some requests so you have data in the metrics.
    ctx := context.Background()
    endpoints := []string{"/posts/1", "/posts/2", "/users/1", "/nonexistent"}
    for _, ep := range endpoints {
        resp, err := client.Get(ctx, ep)
        if err != nil {
            log.Printf("GET %s error: %v", ep, err)
            continue
        }
        resp.Body.Close()
        log.Printf("GET %s -> %d", ep, resp.StatusCode)
    }

    // Wait for interrupt signal before shutting down.
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    if err := srv.Shutdown(shutCtx); err != nil {
        log.Printf("server shutdown error: %v", err)
    }
}
```

After running this, `curl localhost:8080/metrics` returns output like:

```
# HELP myapp_http_client_relay_active_requests Number of HTTP requests currently in flight.
# TYPE myapp_http_client_relay_active_requests gauge
myapp_http_client_relay_active_requests{host="jsonplaceholder.typicode.com",method="GET",service="user-service"} 0
# HELP myapp_http_client_relay_relay_request_duration_seconds HTTP request duration in seconds.
# TYPE myapp_http_client_relay_relay_request_duration_seconds histogram
myapp_http_client_relay_relay_request_duration_seconds_bucket{host="jsonplaceholder.typicode.com",method="GET",service="user-service",status_code="200",le="0.005"} 0
...
# HELP myapp_http_client_relay_relay_requests_total Total number of HTTP requests.
# TYPE myapp_http_client_relay_relay_requests_total counter
myapp_http_client_relay_relay_requests_total{host="jsonplaceholder.typicode.com",method="GET",service="user-service",status_code="200"} 3
myapp_http_client_relay_relay_requests_total{host="jsonplaceholder.typicode.com",method="GET",service="user-service",status_code="404"} 1
```

## Custom Histogram Buckets

The default duration buckets in seconds (`.005` through `10`) cover most HTTP workloads. Override them when your API has a different latency profile.

### Sub-5ms Internal RPC

```go
relayprom.WithDurationBuckets([]float64{
    .0001, .0005, .001, .0025, .005, .01, .025, .05, .1, .25,
})
```

### Long-running Async APIs

```go
relayprom.WithDurationBuckets([]float64{
    1, 2.5, 5, 10, 30, 60, 120, 300, 600,
})
```

### Using prometheus.DefBuckets

To restore the Prometheus default buckets explicitly:

```go
import "github.com/prometheus/client_golang/prometheus"

relayprom.WithDurationBuckets(prometheus.DefBuckets)
```

> **tip**
> Use `prometheus.ExponentialBucketsRange(min, max, count)` to generate evenly distributed exponential buckets without hand-tuning every boundary value.

```go
// 12 buckets from 1ms to 10s distributed exponentially.
buckets, _ := prometheus.LinearBuckets(0.001, 10, 12)
relayprom.WithDurationBuckets(buckets)
```

## Multiple Clients with Separate Metrics

If you run multiple relay clients targeting different services, give each its own registry or differentiate them with const labels:

```go
package main

import (
    "github.com/jhonsferg/relay"
    relayprom "github.com/jhonsferg/relay/ext/prometheus"
    "github.com/prometheus/client_golang/prometheus"
)

func buildClients() (*relay.Client, *relay.Client, error) {
    reg := prometheus.NewRegistry()

    orders, err := relay.New(
        relay.WithBaseURL("https://orders.internal"),
        relayprom.WithPrometheus(
            relayprom.WithRegisterer(reg),
            relayprom.WithConstLabels(prometheus.Labels{"upstream": "orders"}),
        ),
    )
    if err != nil {
        return nil, nil, err
    }

    inventory, err := relay.New(
        relay.WithBaseURL("https://inventory.internal"),
        relayprom.WithPrometheus(
            relayprom.WithRegisterer(reg),
            relayprom.WithConstLabels(prometheus.Labels{"upstream": "inventory"}),
        ),
    )
    if err != nil {
        return nil, nil, err
    }

    return orders, inventory, nil
}
```

> **note**
> Both clients share the same registry in the example above. relay's Prometheus extension uses `MustRegisterOrGet` semantics - if metrics with the same name but different const labels are already registered, the extension creates new label combinations rather than failing.

## Prometheus Alerting Rules

Sample recording and alerting rules for the relay metrics:

```yaml
groups:
  - name: relay_client
    rules:
      - record: job:relay_request_rate5m
        expr: rate(relay_requests_total[5m])

      - record: job:relay_error_rate5m
        expr: |
          rate(relay_requests_total{status_code=~"5.."}[5m])
          /
          rate(relay_requests_total[5m])

      - alert: RelayHighErrorRate
        expr: job:relay_error_rate5m > 0.05
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Relay client error rate above 5% for {{ $labels.host }}"

      - alert: RelaySlowRequests
        expr: histogram_quantile(0.99, rate(relay_request_duration_seconds_bucket[5m])) > 2
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "P99 latency above 2s for {{ $labels.host }}"
```

## See Also

- [Metrics Extension](metrics.md) - Full OpenTelemetry metrics pipeline
- [Tracing Extension](tracing.md) - Distributed tracing
- [Extensions Overview](index.md)
