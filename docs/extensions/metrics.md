# OpenTelemetry Metrics Extension

The `metrics` extension records per-request telemetry using the OpenTelemetry Metrics API. It tracks request counts, durations, and concurrency using instruments that integrate with any OpenTelemetry-compatible backend: Prometheus, Datadog, Honeycomb, Google Cloud Monitoring, and others.

## Installation

```bash
go get github.com/jhonsferg/relay/ext/metrics
```

## Import

```go
import relaymetrics "github.com/jhonsferg/relay/ext/metrics"
```

## Quick Start

```go
package main

import (
    "context"
    "log"

    "github.com/jhonsferg/relay"
    relaymetrics "github.com/jhonsferg/relay/ext/metrics"

    "go.opentelemetry.io/otel"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relaymetrics.WithOTelMetrics(otel.GetMeterProvider()),
    )
    if err != nil {
        log.Fatalf("relay.New: %v", err)
    }
    defer client.Close()

    ctx := context.Background()
    resp, err := client.Get(ctx, "/status")
    if err != nil {
        log.Fatalf("request failed: %v", err)
    }
    defer resp.Body.Close()
    log.Printf("status: %d", resp.StatusCode)
}
```

## API Reference

### `relaymetrics.WithOTelMetrics(mp)`

```go
func WithOTelMetrics(
    mp   metric.MeterProvider,
    opts ...MetricsOption,
) relay.Option
```

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `mp` | `metric.MeterProvider` | The OTel meter provider. Use `otel.GetMeterProvider()` for the global provider. |
| `opts` | `...MetricsOption` | Optional configuration described in the sections below. |

### Metrics Options

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `WithMeterName` | `string` | `"relay"` | Overrides the instrumentation scope name used to create the meter. |
| `WithDurationBuckets` | `[]float64` | See below | Histogram bucket boundaries for the duration histogram. |
| `WithExtraLabels` | `func(*http.Request) []attribute.KeyValue` | none | Adds custom attributes to every data point. |
| `WithHostNormaliser` | `func(host string) string` | identity | Normalises the host label (e.g., strips port numbers). |

## Recorded Metrics

### `relay_request_total`

A counter that increments once per completed request (successful or failed).

- **Instrument type:** Counter (`int64`)
- **Unit:** `{request}`
- **Description:** Total number of HTTP requests executed.

**Labels:**

| Label | Description | Example |
|-------|-------------|---------|
| `method` | HTTP method in upper case | `GET` |
| `host` | Target hostname | `api.example.com` |
| `status_code` | HTTP status code as a string | `200`, `404`, `503` |

When a transport-level error occurs (no response), `status_code` is set to `"error"`.

### `relay_request_duration_ms`

A histogram that records the elapsed time in milliseconds from request start to response body close.

- **Instrument type:** Histogram (`float64`)
- **Unit:** `ms`
- **Description:** HTTP request duration in milliseconds.

**Labels:** same as `relay_request_total`.

**Default bucket boundaries (ms):**

```
0.5, 1, 2.5, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000
```

### `relay_active_requests`

An up-down counter tracking the number of in-flight requests at any moment.

- **Instrument type:** UpDownCounter (`int64`)
- **Unit:** `{request}`
- **Description:** Number of HTTP requests currently in flight.

**Labels:** `method`, `host` (no `status_code` because the request has not completed).

## Complete Example: OTLP HTTP Exporter

This example sets up an OTLP/HTTP metrics exporter, creates a periodic reader, configures the SDK, and wires everything into a relay client.

```go
package main

import (
    "context"
    "log"
    "net/http"
    "time"

    "github.com/jhonsferg/relay"
    relaymetrics "github.com/jhonsferg/relay/ext/metrics"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
    "go.opentelemetry.io/otel/sdk/metric"
    "go.opentelemetry.io/otel/sdk/resource"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func initMeterProvider(ctx context.Context) (*metric.MeterProvider, error) {
    exporter, err := otlpmetrichttp.New(ctx,
        otlpmetrichttp.WithEndpoint("localhost:4318"),
        otlpmetrichttp.WithInsecure(),
    )
    if err != nil {
        return nil, err
    }

    res, err := resource.New(ctx,
        resource.WithAttributes(
            semconv.ServiceName("payment-worker"),
            semconv.ServiceVersion("2.3.1"),
        ),
    )
    if err != nil {
        return nil, err
    }

    mp := metric.NewMeterProvider(
        metric.WithReader(
            metric.NewPeriodicReader(exporter,
                metric.WithInterval(15*time.Second),
            ),
        ),
        metric.WithResource(res),
    )

    otel.SetMeterProvider(mp)
    return mp, nil
}

func main() {
    ctx := context.Background()

    mp, err := initMeterProvider(ctx)
    if err != nil {
        log.Fatalf("initMeterProvider: %v", err)
    }
    defer func() {
        shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        if err := mp.Shutdown(shutCtx); err != nil {
            log.Printf("meter provider shutdown: %v", err)
        }
    }()

    client, err := relay.New(
        relay.WithBaseURL("https://payments.example.com"),
        relay.WithTimeout(30),
        relaymetrics.WithOTelMetrics(
            otel.GetMeterProvider(),
            relaymetrics.WithDurationBuckets([]float64{
                1, 5, 10, 25, 50, 100, 250, 500, 1000, 2000, 5000,
            }),
            relaymetrics.WithExtraLabels(func(req *http.Request) []attribute.KeyValue {
                // Tag metrics with the service tier from a custom header.
                tier := req.Header.Get("X-Service-Tier")
                if tier == "" {
                    tier = "default"
                }
                return []attribute.KeyValue{
                    attribute.String("service_tier", tier),
                }
            }),
        ),
    )
    if err != nil {
        log.Fatalf("relay.New: %v", err)
    }
    defer client.Close()

    // Each request updates relay_request_total, relay_request_duration_ms,
    // and relay_active_requests automatically.
    resp, err := client.Post(ctx, "/charge", map[string]any{
        "amount":   9900,
        "currency": "usd",
    })
    if err != nil {
        log.Fatalf("POST /charge: %v", err)
    }
    defer resp.Body.Close()

    log.Printf("charge status: %d", resp.StatusCode)
}
```

## Histogram Bucket Configuration

Choosing the right histogram buckets is critical for accurate latency percentiles. The default buckets cover a wide range from sub-millisecond to 10 seconds and suit most HTTP clients. For APIs with very different latency profiles, customise them with `WithDurationBuckets`.

### Low-latency Internal APIs

For services with P99 under 50 ms, use fine-grained buckets in the low range:

```go
relaymetrics.WithDurationBuckets([]float64{
    0.1, 0.25, 0.5, 1, 2.5, 5, 10, 25, 50, 100, 250,
})
```

### Batch Processing / Long-running APIs

For APIs that regularly take seconds or even minutes:

```go
relaymetrics.WithDurationBuckets([]float64{
    100, 250, 500, 1000, 2500, 5000, 10000, 30000, 60000, 120000,
})
```

### Explicit Percentile Targets

If you need accurate P50/P90/P99/P99.9, use bucket boundaries that bracket your expected percentile values. For a service with P50=20ms, P90=80ms, P99=300ms, P99.9=900ms:

```go
relaymetrics.WithDurationBuckets([]float64{
    5, 10, 15, 20, 30, 40, 50, 60, 80, 100, 150, 200, 300, 500, 900, 1500, 3000,
})
```

> **tip**
> Use `go.opentelemetry.io/otel/sdk/metric` view configurations if you need different buckets for different instruments without forking the extension. Views apply globally to the meter provider and override per-instrument defaults.

## Adding Extra Labels

Use `WithExtraLabels` to attach dimensions that are specific to your application. Common uses:

- **Tenant ID** from a request header
- **API version** from the URL path prefix
- **Region** from an environment variable

```go
import (
    "net/http"
    "os"

    "go.opentelemetry.io/otel/attribute"
    relaymetrics "github.com/jhonsferg/relay/ext/metrics"
)

region := os.Getenv("AWS_REGION")

relaymetrics.WithExtraLabels(func(req *http.Request) []attribute.KeyValue {
    kvs := []attribute.KeyValue{
        attribute.String("region", region),
    }
    if tenantID := req.Header.Get("X-Tenant-ID"); tenantID != "" {
        kvs = append(kvs, attribute.String("tenant_id", tenantID))
    }
    return kvs
})
```

> **warning**
> High-cardinality labels (such as user IDs or raw URLs) cause metric series explosion. Always use low-cardinality values for labels - normalise paths and IDs before attaching them.

## Host Normalisation

The default `host` label includes the port when it is non-standard (e.g., `api.example.com:8443`). To strip the port and keep only the hostname:

```go
import "strings"

relaymetrics.WithHostNormaliser(func(host string) string {
    if i := strings.LastIndex(host, ":"); i != -1 {
        return host[:i]
    }
    return host
})
```

Or, to replace dynamic hostnames with a static label:

```go
relaymetrics.WithHostNormaliser(func(host string) string {
    switch {
    case strings.HasSuffix(host, ".payments.internal"):
        return "payments-cluster"
    case strings.HasSuffix(host, ".search.internal"):
        return "search-cluster"
    default:
        return host
    }
})
```

## Combining with Tracing

Metrics and tracing extensions compose cleanly. Apply both options to the same client:

```go
client, err := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relaytracing.WithTracing(
        otel.GetTracerProvider(),
        otel.GetTextMapPropagator(),
    ),
    relaymetrics.WithOTelMetrics(
        otel.GetMeterProvider(),
    ),
)
```

The tracing extension wraps the transport first, then the metrics extension wraps that. Each outgoing request creates both a span and increments the counters.

## Using a Non-Global Provider

If your service manages multiple meter providers (e.g., one for internal metrics and one for customer-facing metrics), pass the specific provider directly:

```go
internalMP := metric.NewMeterProvider(...)
externalMP := metric.NewMeterProvider(...)

internalClient, _ := relay.New(
    relay.WithBaseURL("https://internal.example.com"),
    relaymetrics.WithOTelMetrics(internalMP),
)

externalClient, _ := relay.New(
    relay.WithBaseURL("https://public-api.example.com"),
    relaymetrics.WithOTelMetrics(externalMP),
)
```

## See Also

- [Prometheus Extension](prometheus.md) - Expose metrics via a `/metrics` endpoint without a full OTel pipeline
- [Tracing Extension](tracing.md) - Pair request metrics with distributed traces
- [Extensions Overview](index.md)
