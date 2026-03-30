# relay

![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8?style=flat&logo=go)
[![CI](https://github.com/jhonsferg/relay/actions/workflows/ci.yml/badge.svg)](https://github.com/jhonsferg/relay/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/jhonsferg/relay/graph/badge.svg)](https://codecov.io/gh/jhonsferg/relay)
[![Go Report Card](https://goreportcard.com/badge/github.com/jhonsferg/relay)](https://goreportcard.com/report/github.com/jhonsferg/relay)
[![pkg.go.dev](https://pkg.go.dev/badge/github.com/jhonsferg/relay.svg)](https://pkg.go.dev/github.com/jhonsferg/relay)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A **production-grade, declarative HTTP client for Go** with the ergonomics of Python's _requests_, Kotlin's _OkHttp_, and Java's _OpenFeign_ — batteries included.

---

## Installation

```bash
go get github.com/jhonsferg/relay
```

Optional extensions (add only what you need):

```bash
go get github.com/jhonsferg/relay/ext/oauth      # OAuth 2.0 Client Credentials
go get github.com/jhonsferg/relay/ext/tracing    # OpenTelemetry distributed tracing
go get github.com/jhonsferg/relay/ext/metrics    # OpenTelemetry metrics
go get github.com/jhonsferg/relay/ext/prometheus # Prometheus metrics adapter
go get github.com/jhonsferg/relay/ext/brotli     # Brotli (br) decompression
go get github.com/jhonsferg/relay/ext/zap        # go.uber.org/zap logger adapter
go get github.com/jhonsferg/relay/ext/zerolog    # github.com/rs/zerolog logger adapter
go get github.com/jhonsferg/relay/ext/redis      # Redis cache backend (go-redis/v9)
go get github.com/jhonsferg/relay/ext/memcached  # Memcached cache backend (gomemcache)
go get github.com/jhonsferg/relay/ext/jitterbug  # Alternative retry strategies (decorrelated jitter, linear, budget)
go get github.com/jhonsferg/relay/ext/sentry     # Sentry error & breadcrumb capture
go get github.com/jhonsferg/relay/ext/sigv4      # AWS Signature Version 4 request signing
```

---

## Quick Start

```go
import "github.com/jhonsferg/relay"

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relay.WithTimeout(10 * time.Second),
)

resp, err := client.Execute(client.Get("/users/42"))
if err != nil {
    log.Fatal(err)
}
fmt.Println(resp.StatusCode, resp.String())
```

---

## Feature Matrix

| Category               | Feature                                                                                           |
|------------------------|---------------------------------------------------------------------------------------------------|
| **Transport**          | Connection pool tuning, HTTP/2, TLS 1.2+, proxy, custom dialer, DNS overrides                    |
| **Request building**   | Fluent builder, path params, query params, JSON/form/multipart bodies, auth helpers               |
| **Response**           | Buffered body, JSON decode, status helpers, cookies, redirect count, timing breakdown             |
| **Retry**              | Exponential backoff + full jitter, `Retry-After` header, custom predicate, `OnRetry` callback    |
| **Circuit breaker**    | Closed → Open → Half-Open, configurable thresholds, `OnStateChange` callback                     |
| **Rate limiting**      | Client-side token bucket (sustained RPS + burst)                                                  |
| **Caching**            | RFC 7234 (`Cache-Control`, `ETag`, `Last-Modified`), pluggable backend                            |
| **Streaming**          | `ExecuteStream` — unbuffered body for SSE, file downloads, JSONL                                 |
| **Async**              | `ExecuteAsync` (channel) and `ExecuteAsyncCallback` (goroutine + callback)                       |
| **Batch**              | `ExecuteBatch` — concurrent fan-out with bounded concurrency                                      |
| **Middleware**         | Pluggable `http.RoundTripper` chain via `WithTransportMiddleware`                                 |
| **Hooks**              | `OnBeforeRequest` and `OnAfterResponse`                                                           |
| **Lifecycle**          | Graceful `Shutdown` — drains in-flight and streaming requests                                     |
| **Error classes**      | `ClassifyError` — transient / permanent / rate-limited / canceled                                |
| **TLS pinning**        | SHA-256 certificate pinning via `WithCertificatePinning`                                          |
| **Digest auth**        | HTTP Digest Authentication (RFC 7616) via `WithDigestAuth`                                       |
| **Progress**           | Upload/download progress callbacks                                                                |
| **Coalescing**         | Request deduplication for concurrent identical GET/HEAD via `WithRequestCoalescing`               |
| **HAR recording**      | Capture traffic in HAR 1.2 format via `WithHARRecording`                                          |
| **Idempotency key**    | Auto-inject `X-Idempotency-Key` on retries via `WithAutoIdempotencyKey`                           |
| **Timing**             | DNS, TCP, TLS, TTFB, content-transfer breakdown on every `Response`                              |
| **Generics**           | `ExecuteAs[T]` for typed JSON decoding                                                            |
| **Logger**             | Pluggable structured logger (`slog` built-in; `zap`, `zerolog` via ext)                          |
| **OAuth 2.0**          | Client Credentials with auto-refresh → `ext/oauth`                                               |
| **OTel tracing**       | Client spans, W3C propagation → `ext/tracing`                                                    |
| **OTel metrics**       | Request count, duration, active requests → `ext/metrics`                                         |
| **Prometheus**         | Prometheus metrics adapter → `ext/prometheus`                                                    |
| **Brotli**             | Transparent `br` decompression → `ext/brotli`                                                    |
| **zap logger**         | go.uber.org/zap adapter → `ext/zap`                                                              |
| **zerolog logger**     | github.com/rs/zerolog adapter → `ext/zerolog`                                                    |
| **Redis cache**        | Redis-backed `CacheStore` (go-redis/v9), TTL + SCAN/DEL Clear → `ext/redis`                     |
| **Memcached cache**    | Memcached-backed `CacheStore` (gomemcache), TTL, base64 key encoding → `ext/memcached`          |
| **Jitterbug retry**    | Decorrelated jitter, linear backoff, retry-budget strategies → `ext/jitterbug`                  |
| **Sentry**             | Exception capture, 4xx/5xx events, HTTP breadcrumbs, per-request Hub clone → `ext/sentry`       |
| **AWS SigV4**          | AWS Signature Version 4 signing for any AWS service → `ext/sigv4`                               |

---

## Usage

### Creating a client

```go
client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relay.WithTimeout(15*time.Second),
    relay.WithDefaultHeaders(map[string]string{"Accept": "application/json"}),
    relay.WithConnectionPool(100, 20, 50),
    relay.WithRateLimit(50, 10), // 50 req/s, burst 10
)
```

### Making requests

```go
// GET with query params
resp, err := client.Execute(
    client.Get("/search").
        WithQueryParam("q", "gopher").
        WithQueryParam("page", "1"),
)

// POST with JSON body
type CreateOrder struct{ Item string; Qty int }
resp, err = client.Execute(
    client.Post("/orders").WithJSON(CreateOrder{"widget", 42}),
)

// Generic JSON decode
order, resp, err := relay.ExecuteAs[CreateOrder](client, client.Get("/orders/1"))
```

### Retry

```go
client := relay.New(
    relay.WithRetry(&relay.RetryConfig{
        MaxAttempts:      5,
        InitialInterval:  200 * time.Millisecond,
        MaxInterval:      10 * time.Second,
        Multiplier:       2.0,
        RetryableStatus:  []int{429, 502, 503, 504},
        OnRetry: func(attempt int, resp *http.Response, err error) {
            log.Printf("retry %d", attempt)
        },
    }),
)
```

### Circuit breaker

```go
client := relay.New(
    relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
        MaxFailures:  5,
        ResetTimeout: 30 * time.Second,
        OnStateChange: func(from, to relay.CircuitBreakerState) {
            log.Printf("circuit %s → %s", from, to)
        },
    }),
)
```

### Streaming

```go
stream, err := client.ExecuteStream(client.Get("/events"))
if err != nil { log.Fatal(err) }
defer stream.Body.Close()

scanner := bufio.NewScanner(stream.Body)
for scanner.Scan() {
    fmt.Println(scanner.Text())
}
```

### Extensions

All extensions return a `relay.Option` and plug in via the transport middleware chain — no global state, safe for concurrent clients.

#### OAuth 2.0 Client Credentials

```go
import relayoauth "github.com/jhonsferg/relay/ext/oauth"

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relayoauth.WithClientCredentials(relayoauth.Config{
        TokenURL:     "https://auth.example.com/token",
        ClientID:     "my-app",
        ClientSecret: os.Getenv("CLIENT_SECRET"),
        Scopes:       []string{"read", "write"},
    }),
)
```

#### OpenTelemetry Tracing

```go
import relaytracing "github.com/jhonsferg/relay/ext/tracing"

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relaytracing.WithTracing(tracerProvider, propagator), // nil = use global
)
```

#### OpenTelemetry Metrics

```go
import relaymetrics "github.com/jhonsferg/relay/ext/metrics"

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relaymetrics.WithOTelMetrics(meterProvider), // nil = use global
)
```

#### Prometheus

```go
import relayprom "github.com/jhonsferg/relay/ext/prometheus"

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relayprom.WithPrometheus(prometheus.DefaultRegisterer, "myapp"),
)
```

#### Brotli decompression

```go
import relaybrotli "github.com/jhonsferg/relay/ext/brotli"

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relaybrotli.WithBrotliDecompression(),
)
```

#### zap logger

```go
import (
    "go.uber.org/zap"
    "github.com/jhonsferg/relay"
    relayzap "github.com/jhonsferg/relay/ext/zap"
)

logger, _ := zap.NewProduction()
defer logger.Sync()

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relay.WithLogger(relayzap.NewAdapter(logger)),
)
```

#### zerolog logger

```go
import (
    "os"
    "github.com/rs/zerolog"
    "github.com/jhonsferg/relay"
    relayzl "github.com/jhonsferg/relay/ext/zerolog"
)

logger := zerolog.New(os.Stderr).With().Timestamp().Logger()

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relay.WithLogger(relayzl.NewAdapter(logger)),
)
```

#### Redis cache backend

```go
import (
    "github.com/redis/go-redis/v9"
    "github.com/jhonsferg/relay"
    relayredis "github.com/jhonsferg/relay/ext/redis"
)

rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
store := relayredis.NewCacheStore(rdb, "myapp:http-cache:")

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relay.WithCache(store),
)
```

The store honours the TTL from each cached response's `Cache-Control: max-age`
header. `Clear()` uses `SCAN + DEL` with the key prefix — safe for shared Redis
instances. For Redis Cluster, see the package docs for per-shard iteration.

---

## Examples

Runnable examples live in the `examples/` directory:

| Example | What it shows |
|---|---|
| `basic/` | GET, POST, JSON decode, `ExecuteAs[T]`, response helpers |
| `retry/` | Exponential backoff, retry predicates, `OnRetry` callback |
| `circuit_breaker/` | Closed → Open → Half-Open transitions, `OnStateChange` |
| `batch/` | `ExecuteBatch` fan-out, bounded concurrency, context cancel |
| `streaming/` | `ExecuteStream` for SSE / JSONL / file download |
| `oauth2/` | OAuth 2.0 Client Credentials with auto-refresh (`ext/oauth`) |
| `otel/` | OTel tracing + metrics (`ext/tracing`, `ext/metrics`) |
| `prometheus/` | Prometheus metrics, custom registry, `/metrics` handler (`ext/prometheus`) |
| `zap_logger/` | go.uber.org/zap adapter, named loggers, level filtering (`ext/zap`) |
| `zerolog_logger/` | zerolog adapter, JSON output, ConsoleWriter, sub-loggers (`ext/zerolog`) |
| `redis/` | Redis-backed cache store, TTL, shared stores (`ext/redis`) |
| `redis_cache/` | Custom `CacheStore` implementation walkthrough (no ext deps) |
| `har_recording/` | HAR 1.2 capture, `Entries()`, `Export()`, shared recorder |

## Testutil

The `testutil` package provides an in-process mock HTTP server for tests:

```go
import "github.com/jhonsferg/relay/testutil"

srv := testutil.NewMockServer()
defer srv.Close()

srv.Enqueue(testutil.MockResponse{
    Status: 200,
    Body:   `{"id":1}`,
    Headers: map[string]string{"Content-Type": "application/json"},
})

client := relay.New(relay.WithBaseURL(srv.URL()))
resp, err := client.Execute(client.Get("/users/1"))

req, _ := srv.TakeRequest(time.Second)
fmt.Println(req.Method, req.Path)
```

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Issues and PRs welcome.

---

## License

MIT — see [LICENSE](LICENSE).
