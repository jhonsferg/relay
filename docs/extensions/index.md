# Extensions

The relay library ships with a lean core and a rich set of optional extensions. Extensions are distributed as separate Go modules under the `github.com/jhonsferg/relay/ext/` path. You import only the extensions you need, keeping your binary small and your dependency graph minimal.

Extensions integrate with relay through the standard `Option` pattern. Every extension exposes one or more functions that return an `Option` value. You pass those options to `relay.New(...)` or to individual request builders, and relay wires everything together automatically.

## Available Extensions

| Extension | Import Path | Purpose | Install Command |
|-----------|-------------|---------|----------------|
| tracing | `github.com/jhonsferg/relay/ext/tracing` | OpenTelemetry distributed tracing | `go get github.com/jhonsferg/relay/ext/tracing` |
| metrics | `github.com/jhonsferg/relay/ext/metrics` | OpenTelemetry metrics | `go get github.com/jhonsferg/relay/ext/metrics` |
| prometheus | `github.com/jhonsferg/relay/ext/prometheus` | Prometheus metrics exporter | `go get github.com/jhonsferg/relay/ext/prometheus` |
| sentry | `github.com/jhonsferg/relay/ext/sentry` | Sentry error and performance tracking | `go get github.com/jhonsferg/relay/ext/sentry` |
| http3 | `github.com/jhonsferg/relay/ext/http3` | HTTP/3 QUIC transport | `go get github.com/jhonsferg/relay/ext/http3` |
| websocket | `github.com/jhonsferg/relay/ext/websocket` | WebSocket (legacy, use core) | `go get github.com/jhonsferg/relay/ext/websocket` |
| grpc | `github.com/jhonsferg/relay/ext/grpc` | gRPC-HTTP/JSON bridge | `go get github.com/jhonsferg/relay/ext/grpc` |
| graphql | `github.com/jhonsferg/relay/ext/graphql` | GraphQL query/mutation helpers | `go get github.com/jhonsferg/relay/ext/graphql` |
| oauth | `github.com/jhonsferg/relay/ext/oauth` | OAuth2 flows (PKCE, client creds) | `go get github.com/jhonsferg/relay/ext/oauth` |
| cache | `github.com/jhonsferg/relay/ext/cache` | Redis response caching | `go get github.com/jhonsferg/relay/ext/cache` |
| mock | `github.com/jhonsferg/relay/ext/mock` | Mock transport for testing | `go get github.com/jhonsferg/relay/ext/mock` |
| sigv4 | `github.com/jhonsferg/relay/ext/sigv4` | AWS SigV4 request signing | `go get github.com/jhonsferg/relay/ext/sigv4` |
| openapi | `github.com/jhonsferg/relay/ext/openapi` | OpenAPI spec validation | `go get github.com/jhonsferg/relay/ext/openapi` |
| brotli | `github.com/jhonsferg/relay/ext/brotli` | Brotli compression support | `go get github.com/jhonsferg/relay/ext/brotli` |
| zap | `github.com/jhonsferg/relay/ext/zap` | Uber Zap logger integration | `go get github.com/jhonsferg/relay/ext/zap` |
| zerolog | `github.com/jhonsferg/relay/ext/zerolog` | Zerolog structured logging | `go get github.com/jhonsferg/relay/ext/zerolog` |
| logrus | `github.com/jhonsferg/relay/ext/logrus` | Logrus logger integration | `go get github.com/jhonsferg/relay/ext/logrus` |
| jitterbug | `github.com/jhonsferg/relay/ext/jitterbug` | Jitter timing utilities | `go get github.com/jhonsferg/relay/ext/jitterbug` |
| compress | `github.com/jhonsferg/relay/ext/compress` | zstd compression (with dictionary support) | `go get github.com/jhonsferg/relay/ext/compress` |

## Quick Start: Using Multiple Extensions Together

The example below shows how to compose several extensions into a single client. All options are additive - they do not conflict with each other.

```go
package main

import (
    "context"
    "log"
    "net/http"

    "github.com/jhonsferg/relay"
    relaymetrics  "github.com/jhonsferg/relay/ext/metrics"
    relayprom     "github.com/jhonsferg/relay/ext/prometheus"
    relaytracing  "github.com/jhonsferg/relay/ext/tracing"
    relayzap      "github.com/jhonsferg/relay/ext/zap"

    "go.opentelemetry.io/otel"
    "go.uber.org/zap"
)

func main() {
    logger, err := zap.NewProduction()
    if err != nil {
        log.Fatalf("failed to build zap logger: %v", err)
    }
    defer logger.Sync()

    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTimeout(30),
        relayzap.WithZap(logger),
        relaytracing.WithTracing(otel.GetTracerProvider(), otel.GetTextMapPropagator()),
        relaymetrics.WithOTelMetrics(otel.GetMeterProvider()),
        relayprom.WithPrometheus(
            relayprom.WithNamespace("myapp"),
        ),
    )
    if err != nil {
        log.Fatalf("failed to create relay client: %v", err)
    }

    ctx := context.Background()
    resp, err := client.Get(ctx, "/users/42")
    if err != nil {
        log.Fatalf("request failed: %v", err)
    }
    defer resp.Body.Close()

    log.Printf("status: %d", resp.StatusCode)
}
```

## Extension Architecture

Every extension in relay is implemented as one or more **Option functions**. An `Option` is simply a function with the signature:

```go
type Option func(*Client) error
```

When you call `relay.New(opts...)`, relay iterates over the provided options in order, calling each one against the internal `*Client`. Each option can modify the client's transport chain, set headers, register hooks, or attach middleware.

### The Transport Chain

The relay core uses a layered `http.RoundTripper` interface. Extensions typically wrap the existing transport with their own logic:

```go
// Conceptual example of how an extension wraps the transport.
// This is the pattern used internally by every relay extension.
package myext

import (
    "net/http"
    "github.com/jhonsferg/relay"
)

type loggingTransport struct {
    base http.RoundTripper
}

func (t *loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    // pre-request logic
    resp, err := t.base.RoundTrip(req)
    // post-request logic
    return resp, err
}

// WithLogging is an Option that wraps the existing transport.
func WithLogging() relay.Option {
    return func(c *relay.Client) error {
        c.Transport = &loggingTransport{base: c.Transport}
        return nil
    }
}
```

Because extensions wrap the transport rather than replace it, the order in which you supply options to `relay.New` matters. Options applied later wrap options applied earlier. The innermost layer runs closest to the actual HTTP dial.

### Hooks vs. Transport Wrapping

Some extensions use relay's hook system instead of transport wrapping. Hooks are callbacks triggered at specific points in the request lifecycle:

| Hook | Trigger point |
|------|--------------|
| `OnRequest` | Before the request is sent |
| `OnResponse` | After a successful response is received |
| `OnError` | After a transport-level error |
| `OnRetry` | Before each retry attempt |

Extensions that only need to observe requests (such as loggers) typically use hooks. Extensions that need to modify requests or responses (such as signing or caching) use transport wrapping.

### Lifecycle and Cleanup

Extensions that allocate resources (connections, goroutines, etc.) implement `relay.Closer`. When you call `client.Close()`, relay calls `Close()` on every registered closer in reverse registration order, giving extensions a chance to flush buffers and release connections cleanly.

```go
client, err := relay.New(
    relaytracing.WithTracing(tp, propagator),
)
if err != nil {
    log.Fatal(err)
}
defer client.Close() // flushes tracing spans and other extension resources
```

## Writing Custom Extensions

You can write your own extension using the same patterns as the built-in ones. A custom extension is just a Go package that exports one or more `Option`-returning functions.

### Minimal Example: Request ID Injector

This extension adds a unique `X-Request-ID` header to every outgoing request.

```go
package reqid

import (
    "crypto/rand"
    "encoding/hex"
    "net/http"

    "github.com/jhonsferg/relay"
)

type requestIDTransport struct {
    base   http.RoundTripper
    header string
}

func (t *requestIDTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    // Clone the request so we do not mutate the caller's copy.
    r2 := req.Clone(req.Context())
    if r2.Header == nil {
        r2.Header = make(http.Header)
    }
    r2.Header.Set(t.header, newID())
    return t.base.RoundTrip(r2)
}

func newID() string {
    b := make([]byte, 8)
    _, _ = rand.Read(b)
    return hex.EncodeToString(b)
}

// WithRequestID injects a random request ID into every outgoing request
// using the specified header name. Pass an empty string to use the
// default "X-Request-ID".
func WithRequestID(header string) relay.Option {
    return func(c *relay.Client) error {
        if header == "" {
            header = "X-Request-ID"
        }
        c.Transport = &requestIDTransport{
            base:   c.Transport,
            header: header,
        }
        return nil
    }
}
```

Usage:

```go
client, err := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    reqid.WithRequestID(""), // uses default "X-Request-ID"
)
```

### Example: Per-Request Extension Options

Extensions can also accept per-request configuration using relay's request builder pattern. A request-level option follows the same signature as a client option but is applied to a cloned request context rather than the shared client.

```go
package priority

import (
    "fmt"
    "github.com/jhonsferg/relay"
)

// WithPriority sets a priority tier header on a single request.
func WithPriority(level int) relay.RequestOption {
    return func(req *relay.Request) error {
        if level < 1 || level > 5 {
            return fmt.Errorf("priority must be between 1 and 5, got %d", level)
        }
        req.Header.Set("X-Priority", fmt.Sprintf("%d", level))
        return nil
    }
}
```

Usage at the call site:

```go
resp, err := client.Get(ctx, "/jobs/heavy",
    priority.WithPriority(3),
)
```

### Extension Best Practices

- **Return errors from the Option function** for invalid configuration rather than panicking. relay will surface the error from `relay.New`.
- **Clone requests** before modifying them. The `req.Clone(ctx)` method returns a shallow copy with independent headers.
- **Use `context.Value` sparingly** in transport wrappers. Prefer explicit configuration at construction time.
- **Respect `req.Context().Done()`** in long-running transport operations to support cancellation.
- **Document the import alias** your extension expects (e.g., `relaytracing`, `relayprom`) to avoid collisions with other packages.
- **Register a `Closer`** if your extension manages a background goroutine or connection pool, so `client.Close()` can clean up properly.

## Extension Compatibility Matrix

Extensions are versioned independently. The table below shows the minimum relay core version required for each extension.

| Extension | Min relay version | Notes |
|-----------|------------------|-------|
| tracing | v0.3.0 | Requires OTel SDK >= 1.24 |
| metrics | v0.3.0 | Requires OTel SDK >= 1.24 |
| prometheus | v0.2.0 | Compatible with prometheus/client_golang v1 and v2 |
| sentry | v0.3.0 | Requires sentry-go >= 0.27 |
| http3 | v0.4.0 | Requires quic-go >= 0.42 |
| websocket | v0.1.0 | Legacy - prefer core ExecuteWebSocket |
| grpc | v0.3.0 | Requires google.golang.org/grpc >= 1.62 |
| graphql | v0.2.0 | No heavy dependencies |
| oauth | v0.2.0 | Uses golang.org/x/oauth2 |
| cache | v0.3.0 | Requires redis/go-redis v9 |
| mock | v0.1.0 | Test-only, no production deps |
| sigv4 | v0.3.0 | Requires aws-sdk-go-v2 |
| openapi | v0.4.0 | Requires kin-openapi >= 0.124 |
| brotli | v0.2.0 | Requires andybalholm/brotli |
| zap | v0.2.0 | Requires go.uber.org/zap >= 1.27 |
| zerolog | v0.2.0 | Requires rs/zerolog >= 1.32 |
| logrus | v0.2.0 | Requires sirupsen/logrus >= 1.9 |
| jitterbug | v0.3.0 | No external dependencies |

## See Also

- [Tracing Extension](tracing.md) - Detailed OpenTelemetry tracing guide
- [Metrics Extension](metrics.md) - OpenTelemetry metrics guide
- [Prometheus Extension](prometheus.md) - Prometheus metrics guide
- [Sentry Extension](sentry.md) - Error and performance tracking guide
- [HTTP/3 Extension](http3.md) - QUIC/HTTP3 transport guide
- [WebSocket Extension](websocket.md) - WebSocket migration guide
- [Compress Extension](compress.md) - zstd compression and dictionary guide
