# OpenTelemetry Extension (`ext/otel`)

The `ext/otel` module instruments every relay request with OpenTelemetry **tracing** and **metrics** using the standard `go.opentelemetry.io/otel` SDK.

[![Go Reference](https://pkg.go.dev/badge/github.com/jhonsferg/relay/ext/otel.svg)](https://pkg.go.dev/github.com/jhonsferg/relay/ext/otel)

## Installation

```bash
go get github.com/jhonsferg/relay/ext/otel
```

## Usage

```go
import (
    "go.opentelemetry.io/otel/metric"
    "go.opentelemetry.io/otel/trace"
    "github.com/jhonsferg/relay"
    relayotel "github.com/jhonsferg/relay/ext/otel"
)

// Use your existing tracer and meter, or pass nil to use the global providers.
client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relayotel.WithOtel(tracer, meter),
)
```

## Options

| Option | Description |
|---|---|
| `WithTracing(tracer)` | Creates a client span per request. Pass `nil` for the global `TracerProvider`. |
| `WithMetrics(meter)` | Records request duration and count. Pass `nil` for the global `MeterProvider`. |
| `WithOtel(tracer, meter)` | Enables both tracing and metrics in a single option. |

## Tracing

Each outgoing request creates a span named **`HTTP {METHOD}`** (e.g. `HTTP GET`) with these attributes:

| Attribute | Description |
|---|---|
| `http.request.method` | HTTP method (semconv v1.26) |
| `http.url` | Full request URL |
| `http.response.status_code` | Response status code |
| `http.response_content_length` | Response body size (when `Content-Length` header is present) |

Transport errors are recorded via `span.RecordError(err)` and the span status is set to `codes.Error`.

## Metrics

| Instrument | Type | Labels | Description |
|---|---|---|---|
| `http.client.request.duration` | Histogram (ms) | `http.request.method` | Round-trip latency in milliseconds |
| `http.client.requests.total` | Counter | `http.request.method`, `http.response.status_code` | Total requests including errors |

## Integration with OpenTelemetry SDK

```go
import (
    "go.opentelemetry.io/otel"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// Set up your exporters...
tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter))
mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

otel.SetTracerProvider(tp)
otel.SetMeterProvider(mp)

// Pass nil to use the global providers configured above.
client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relayotel.WithOtel(nil, nil),
)
```

## Using Only Tracing or Only Metrics

```go
// Tracing only
client := relay.New(relayotel.WithTracing(tracer))

// Metrics only
client := relay.New(relayotel.WithMetrics(meter))
```

## Composing with Other Middleware

The `ext/otel` options compose cleanly with other relay middleware:

```go
client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relayotel.WithOtel(tracer, meter),     // OTel first  -  wraps outermost
    relay.WithRequestLogger(logger),        // Logger sees final status
)
```
