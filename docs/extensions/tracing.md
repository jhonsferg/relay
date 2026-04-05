# OpenTelemetry Tracing Extension

The `tracing` extension adds OpenTelemetry distributed tracing to every HTTP request made by a relay client. Each request becomes a child span under whatever span is active in the provided context, forming a complete trace that includes request preparation, network I/O, and response processing.

## Installation

```bash
go get github.com/jhonsferg/relay/ext/tracing
```

## Import

```go
import relaytracing "github.com/jhonsferg/relay/ext/tracing"
```

## Quick Start

```go
package main

import (
    "context"
    "log"

    "github.com/jhonsferg/relay"
    relaytracing "github.com/jhonsferg/relay/ext/tracing"

    "go.opentelemetry.io/otel"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relaytracing.WithTracing(
            otel.GetTracerProvider(),
            otel.GetTextMapPropagator(),
        ),
    )
    if err != nil {
        log.Fatalf("relay.New: %v", err)
    }
    defer client.Close()

    ctx := context.Background()
    resp, err := client.Get(ctx, "/users/1")
    if err != nil {
        log.Fatalf("GET /users/1: %v", err)
    }
    defer resp.Body.Close()
}
```

## API Reference

### `relaytracing.WithTracing(tp, propagator)`

`WithTracing` is the primary option. It wraps the relay transport with an OpenTelemetry-aware layer that:

1. Starts a child span for each outgoing request.
2. Injects the span context into request headers using the supplied propagator.
3. Records span attributes (method, URL, status code, error).
4. Ends the span when the response body is fully consumed or closed.

```go
func WithTracing(
    tp   trace.TracerProvider,
    prop propagation.TextMapPropagator,
    opts ...TracingOption,
) relay.Option
```

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `tp` | `trace.TracerProvider` | The OTel tracer provider. Use `otel.GetTracerProvider()` for the global provider. |
| `prop` | `propagation.TextMapPropagator` | The propagator used to inject span context into headers. |
| `opts` | `...TracingOption` | Optional configuration, described below. |

### Tracing Options

| Option | Signature | Default | Description |
|--------|-----------|---------|-------------|
| `WithSpanNameFormatter` | `func(req *http.Request) string` | `"HTTP {METHOD}"` | Customises the span name. |
| `WithAttributeFilter` | `func(kv attribute.KeyValue) bool` | allow all | Filters which attributes are recorded. |
| `WithStatusCodeAsError` | `func(code int) bool` | `code >= 500` | Controls when a status code marks the span as an error. |

## Span Names and Attributes

By default, each span is named using the HTTP method in upper case, e.g. `"HTTP GET"`. You can override this with `WithSpanNameFormatter`.

### Standard Attributes

The extension records the following semantic convention attributes on every span:

| Attribute | OTel key | Example value |
|-----------|----------|--------------|
| HTTP method | `http.request.method` | `"GET"` |
| Full URL | `url.full` | `"https://api.example.com/users/1"` |
| URL scheme | `url.scheme` | `"https"` |
| Host | `server.address` | `"api.example.com"` |
| Port | `server.port` | `443` |
| Status code | `http.response.status_code` | `200` |
| Error type | `error.type` | `"connection_refused"` |
| Request content length | `http.request.body.size` | `512` |
| Response content length | `http.response.body.size` | `1024` |

### Span Status

- If the response status code is `>= 500` (or matches your custom `WithStatusCodeAsError` function), the span status is set to `codes.Error`.
- If a transport-level error occurs (timeout, DNS failure, TLS error), the span status is set to `codes.Error` and the error message is recorded as the `error.type` attribute.
- All other cases set the span status to `codes.Ok`.

## W3C TraceContext Propagation

The W3C TraceContext format (`traceparent` / `tracestate` headers) is the recommended propagator. Use `propagation.TraceContext{}` from `go.opentelemetry.io/otel/propagation`:

```go
package main

import (
    "context"
    "log"

    "github.com/jhonsferg/relay"
    relaytracing "github.com/jhonsferg/relay/ext/tracing"

    "go.opentelemetry.io/otel/propagation"
)

func newTracedClient(tp trace.TracerProvider) (*relay.Client, error) {
    prop := propagation.NewCompositeTextMapPropagator(
        propagation.TraceContext{},
        propagation.Baggage{},
    )
    return relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relaytracing.WithTracing(tp, prop),
    )
}
```

The upstream service receives the `traceparent` header and can join the same trace.

## B3 Propagation

For services that use the Zipkin B3 format, swap in the B3 propagator from `go.opentelemetry.io/contrib/propagators/b3`:

```go
package main

import (
    "github.com/jhonsferg/relay"
    relaytracing "github.com/jhonsferg/relay/ext/tracing"

    "go.opentelemetry.io/contrib/propagators/b3"
    "go.opentelemetry.io/otel/propagation"
)

func newB3Client(tp trace.TracerProvider) (*relay.Client, error) {
    prop := propagation.NewCompositeTextMapPropagator(
        b3.New(b3.WithInjectEncoding(b3.B3MultipleHeader)),
    )
    return relay.New(
        relay.WithBaseURL("https://legacy-service.internal"),
        relaytracing.WithTracing(tp, prop),
    )
}
```

> **tip**
> You can compose multiple propagators. Pass both `propagation.TraceContext{}` and `b3.New(...)` to `propagation.NewCompositeTextMapPropagator` to interoperate with mixed environments.

## Complete Example: Jaeger via OTLP/gRPC

This example configures a Jaeger-compatible OTLP exporter, sets up the SDK with a tail-based sampler, registers the global provider, and creates a traced relay client.

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/jhonsferg/relay"
    relaytracing "github.com/jhonsferg/relay/ext/tracing"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/propagation"
    "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

func initTracer(ctx context.Context) (*sdktrace.TracerProvider, error) {
    // Dial the OTLP collector (Jaeger, etc.) without TLS for local dev.
    conn, err := grpc.NewClient(
        "localhost:4317",
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    if err != nil {
        return nil, err
    }

    exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
    if err != nil {
        return nil, err
    }

    res, err := resource.New(ctx,
        resource.WithAttributes(
            semconv.ServiceName("my-api-client"),
            semconv.ServiceVersion("1.0.0"),
            semconv.DeploymentEnvironmentName("production"),
        ),
    )
    if err != nil {
        return nil, err
    }

    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter,
            sdktrace.WithBatchTimeout(5*time.Second),
        ),
        sdktrace.WithResource(res),
        sdktrace.WithSampler(
            sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1)), // 10% sampling
        ),
    )

    // Set the global provider and propagator.
    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(
        propagation.NewCompositeTextMapPropagator(
            propagation.TraceContext{},
            propagation.Baggage{},
        ),
    )

    return tp, nil
}

func main() {
    ctx := context.Background()

    tp, err := initTracer(ctx)
    if err != nil {
        log.Fatalf("initTracer: %v", err)
    }
    defer func() {
        shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        if err := tp.Shutdown(shutCtx); err != nil {
            log.Printf("tracer shutdown error: %v", err)
        }
    }()

    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTimeout(15),
        relaytracing.WithTracing(
            otel.GetTracerProvider(),
            otel.GetTextMapPropagator(),
            relaytracing.WithSpanNameFormatter(func(req *http.Request) string {
                // Use "METHOD /path" as the span name, omitting query strings.
                return req.Method + " " + req.URL.Path
            }),
        ),
    )
    if err != nil {
        log.Fatalf("relay.New: %v", err)
    }
    defer client.Close()

    // Start a root span to act as the parent.
    tracer := otel.Tracer("main")
    ctx, span := tracer.Start(ctx, "fetch-user")
    defer span.End()

    resp, err := client.Get(ctx, "/users/42")
    if err != nil {
        span.RecordError(err)
        log.Fatalf("GET /users/42: %v", err)
    }
    defer resp.Body.Close()

    log.Printf("status: %d", resp.StatusCode)
}
```

## Sampling Configuration

Sampling decisions are made by the `sdktrace.Sampler` you attach to the provider, not by the relay extension itself. The extension always creates a span; the SDK decides whether to record and export it.

### Common Samplers

```go
// Always sample - useful for development and low-traffic services.
sdktrace.WithSampler(sdktrace.AlwaysSample())

// Never sample - disables tracing entirely without removing the extension.
sdktrace.WithSampler(sdktrace.NeverSample())

// Sample 5% of root spans; respect the parent's decision for child spans.
sdktrace.WithSampler(
    sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.05)),
)

// Sample all error spans plus 1% of successful spans.
sdktrace.WithSampler(
    sdktrace.ParentBased(
        &errorAwareSampler{base: sdktrace.TraceIDRatioBased(0.01)},
    ),
)
```

### Custom Sampler: Always Sample Errors

```go
package main

import (
    "go.opentelemetry.io/otel/sdk/trace"
)

// errorAwareSampler samples every request that results in a 5xx status,
// falling back to a base sampler for successful requests.
type errorAwareSampler struct {
    base sdktrace.Sampler
}

func (s *errorAwareSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
    for _, attr := range p.Attributes {
        if attr.Key == "http.response.status_code" {
            if attr.Value.AsInt64() >= 500 {
                return sdktrace.SamplingResult{Decision: sdktrace.RecordAndSample}
            }
        }
    }
    return s.base.ShouldSample(p)
}

func (s *errorAwareSampler) Description() string {
    return "ErrorAwareSampler"
}
```

> **note**
> Span attributes set during `RoundTrip` (such as `http.response.status_code`) are available to the sampler only if the sampler runs after the transport completes. The relay tracing extension sets attributes before calling `span.End()`, so custom samplers that inspect response attributes work correctly.

## Span Customisation

### Custom Span Name

```go
relaytracing.WithSpanNameFormatter(func(req *http.Request) string {
    // Group spans by resource pattern rather than exact path.
    // /users/123 and /users/456 both become "GET /users/{id}".
    return req.Method + " " + normalisePath(req.URL.Path)
})
```

### Filtering Sensitive Attributes

Prevent the full URL (which may contain tokens in query parameters) from being recorded:

```go
relaytracing.WithAttributeFilter(func(kv attribute.KeyValue) bool {
    switch string(kv.Key) {
    case "url.full", "url.query":
        return false
    }
    return true
})
```

## Integration with net/http Servers

When relay is used inside an HTTP handler, propagate the incoming trace context so relay's outgoing span becomes a child of the inbound span:

```go
func handler(w http.ResponseWriter, r *http.Request) {
    // r.Context() already carries the inbound span if you use otelhttp middleware.
    resp, err := client.Get(r.Context(), "/downstream/resource")
    if err != nil {
        http.Error(w, "upstream error", http.StatusBadGateway)
        return
    }
    defer resp.Body.Close()
    // ...
}
```

Use `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` to instrument your server and automatically populate `r.Context()` with the active span.

## See Also

- [Metrics Extension](metrics.md) - Record request counters and durations alongside traces
- [Sentry Extension](sentry.md) - Capture errors in Sentry while tracing with OTel
- [Extensions Overview](index.md)
