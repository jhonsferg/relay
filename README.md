<div align="center">

# Relay

**A production-grade, declarative HTTP client for Go with the ergonomics of Python's *requests* and the resilience of *Resilience4j*.**

[![Go Version](https://img.shields.io/badge/Go-1.22%2B-00ADD8?style=for-the-badge&logo=go)](https://pkg.go.dev/github.com/jhonsferg/relay)
[![CI](https://img.shields.io/github/actions/workflows/ci.yml?style=for-the-badge&logo=github)](https://github.com/jhonsferg/relay/actions)
[![codecov](https://img.shields.io/codecov/c/github/jhonsferg/relay?style=for-the-badge&logo=codecov)](https://codecov.io/gh/jhonsferg/relay)
[![Go Report Card](https://goreportcard.com/badge/github.com/jhonsferg/relay?style=for-the-badge)](https://goreportcard.com/report/github.com/jhonsferg/relay)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg?style=for-the-badge)](LICENSE)

---

**[Installation](#-installation) • [Quick Start](#-quick-start) • [Core Features](#-core-features) • [Extensions](#-extension-ecosystem) • [Testing](#-testing) • [Examples](#-examples) • [Performance](#-performance)**

</div>

## Overview

**Relay** is designed for developers who need more than just `http.Client`. It provides a fluent, batteries-included API for building resilient distributed systems. Retries, circuit breaking, caching, rate limiting, streaming, and observability are all built in — allowing you to focus on your business logic.

The core module has **zero external dependencies**. Every integration (Redis, OTel, Prometheus, gRPC, etc.) lives in its own optional extension module so you only pull in what you actually use.

---

## Architecture

### Request / Response Lifecycle

```
Execute(Request)
  │
  ├─ OnBeforeRequest hooks     (auth, rate limiting, mutation)
  │
  ├─ Rate limiter              (token bucket / sliding window)
  │
  ├─ Circuit breaker           (open → reject immediately)
  │
  ├─ Retry loop                (backoff + jitter on failure)
  │    │
  │    └─ Transport middleware stack   (cache, tracing, auth signing, …)
  │         │
  │         └─ net/http → network
  │
  ├─ OnAfterResponse hooks     (logging, metrics, error promotion)
  │
  └─ *Response                 (body, status, timing, headers)
```

Every layer is opt-in via `relay.Option` — compose exactly the behaviour your service needs.

---

## Installation

```bash
go get github.com/jhonsferg/relay
```

Pick only the extensions you need:

```bash
# Observability
go get github.com/jhonsferg/relay/ext/tracing        # OpenTelemetry tracing
go get github.com/jhonsferg/relay/ext/metrics        # OpenTelemetry metrics
go get github.com/jhonsferg/relay/ext/prometheus     # Native Prometheus metrics
go get github.com/jhonsferg/relay/ext/sentry         # Sentry error reporting

# Caching
go get github.com/jhonsferg/relay/ext/redis          # Redis cache backend
go get github.com/jhonsferg/relay/ext/memcached      # Memcached cache backend
go get github.com/jhonsferg/relay/ext/cache/lru      # In-memory LRU cache
go get github.com/jhonsferg/relay/ext/cache/twolevel # Two-level (L1+L2) cache

# Security & Cloud
go get github.com/jhonsferg/relay/ext/oauth          # OAuth 2.0 Client Credentials
go get github.com/jhonsferg/relay/ext/sigv4          # AWS Signature V4
go get github.com/jhonsferg/relay/ext/grpc           # gRPC-Gateway metadata

# Resilience
go get github.com/jhonsferg/relay/ext/jitterbug               # Advanced backoff strategies
go get github.com/jhonsferg/relay/ext/breaker/gobreaker       # sony/gobreaker circuit breaker
go get github.com/jhonsferg/relay/ext/ratelimit/distributed   # Redis sliding-window rate limiter

# Logging
go get github.com/jhonsferg/relay/ext/zap      # go.uber.org/zap adapter
go get github.com/jhonsferg/relay/ext/zerolog  # rs/zerolog adapter
go get github.com/jhonsferg/relay/ext/logrus   # sirupsen/logrus adapter

# Validation & Interop
go get github.com/jhonsferg/relay/ext/openapi  # OpenAPI 3.x request/response validation

# Compression
go get github.com/jhonsferg/relay/ext/brotli   # Brotli decompression

# Testing
go get github.com/jhonsferg/relay/ext/mock     # Programmable mock transport
```

---

## Quick Start

```go
package main

import (
    "fmt"
    "time"

    "github.com/jhonsferg/relay"
)

type User struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTimeout(10*time.Second),
        relay.WithRetry(&relay.RetryConfig{MaxAttempts: 3}),
    )

    // Typed JSON decode with generics
    user, resp, err := relay.ExecuteAs[User](client, client.Get("/users/42"))
    if err != nil {
        panic(err)
    }

    fmt.Printf("%d %s — fetched in %v\n", user.ID, user.Name, resp.Timing.Total)
}
```

---

## Core Features

### Request Building

Relay's fluent builder covers every HTTP scenario without boilerplate:

```go
resp, err := client.Post("/orders/{id}").
    WithPathParam("id", "ord-42").
    WithHeader("X-Idempotency-Key", "req-abc").
    WithQueryParam("expand", "items").
    WithJSON(orderPayload).
    WithTimeout(5 * time.Second).
    WithTag("operation", "CreateOrder").   // client-side label, not sent
    Execute()
```

**Supported body types**

| Method | Content-Type |
|--------|-------------|
| `WithJSON(v)` | `application/json` |
| `WithFormData(map)` | `application/x-www-form-urlencoded` |
| `WithMultipart(fields)` | `multipart/form-data` |
| `WithBody([]byte)` | *(caller sets Content-Type)* |
| `WithBodyReader(r)` | *(caller sets Content-Type)* |

### Request Cloning

Clone a base request and vary headers, params, or bodies without re-building from scratch:

```go
base := client.Post("/events").
    WithHeader("Content-Type", "application/json").
    WithTag("service", "billing")

for _, event := range events {
    req := base.Clone().WithJSON(event)
    go client.Execute(req)
}
```

### Per-Request Body Size Limit

Override the client-level limit for a single request:

```go
// Allow up to 50 MB for this download
resp, err := client.Execute(
    client.Get("/reports/large.csv").WithMaxBodySize(50 << 20),
)

// No limit at all for this specific request
resp, err = client.Execute(
    client.Get("/stream").WithMaxBodySize(-1),
)
```

### Resilience

#### Exponential Backoff Retries

```go
relay.WithRetry(&relay.RetryConfig{
    MaxAttempts:     4,
    InitialInterval: 100 * time.Millisecond,
    MaxInterval:     30 * time.Second,
    Multiplier:      2.0,
    RetryableStatus: []int{429, 502, 503, 504},
})
```

#### Circuit Breaker

```go
relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
    MaxFailures:  5,
    ResetTimeout: 30 * time.Second,
    OnStateChange: func(from, to relay.CircuitBreakerState) {
        log.Printf("circuit: %s → %s", from, to)
    },
})
```

#### Automatic Health Check Recovery

When the circuit breaker opens, a background goroutine probes a health endpoint and
resets the breaker automatically without waiting for `ResetTimeout`:

```go
relay.WithHealthCheck(
    "https://api.example.com/health", // probe URL
    5*time.Second,                    // poll interval
    2*time.Second,                    // probe timeout
    200,                              // expected status
)
```

#### Client-Side Rate Limiting

```go
relay.WithRateLimit(100, time.Second) // 100 requests per second
```

### DNS Caching

Reduce resolver latency on high-concurrency workloads by caching DNS results:

```go
relay.WithDNSCache(30 * time.Second) // cache each hostname for 30 s
```

IP-literal addresses bypass the cache. Cache entries are refreshed lazily on expiry.

### Streaming

#### Server-Sent Events (SSE)

```go
err := client.ExecuteSSE(
    client.Get("/events").WithHeader("Accept", "text/event-stream"),
    func(event relay.SSEEvent) bool {
        fmt.Printf("[%s] %s\n", event.Event, event.Data)
        return true // return false to stop
    },
)
```

`SSEEvent` carries `ID`, `Event`, `Data`, and `Retry` fields per the W3C spec.
Multi-line `data:` fields are concatenated with `\n`.

#### JSONL / NDJSON Streaming

```go
err := relay.ExecuteAsStream[LogEntry](client, client.Get("/logs"), func(entry LogEntry) bool {
    fmt.Println(entry.Message)
    return entry.Level != "FATAL" // stop on fatal
})
```

Each newline-delimited JSON line is decoded directly into `T` without buffering the full body.

#### Raw Streaming

```go
stream, err := client.ExecuteStream(client.Get("/video"))
defer stream.Body.Close()
io.Copy(dst, stream.Body)
```

### Caching (Built-in)

Relay implements RFC 7234 HTTP caching semantics. Plug in any `CacheStore`:

```go
client := relay.New(
    relay.WithCache(store),              // any CacheStore implementation
    relay.WithCacheMaxAge(5*time.Minute), // global TTL override
)
```

Conditional requests (`ETag`, `Last-Modified`) and `Cache-Control` directives are
handled automatically.

### Request Coalescing

Collapse concurrent identical GET requests into a single upstream call:

```go
relay.WithCoalescing() // multiple goroutines get the same response
```

### Response Timing

```go
resp, _ := client.Execute(req)
t := resp.Timing

fmt.Printf(
    "DNS: %v  TCP: %v  TLS: %v  TTFB: %v  Total: %v\n",
    t.DNSLookup, t.TCPConnection, t.TLSHandshake, t.ServerProcessing, t.Total,
)
```

### Hooks

```go
client := relay.New(
    relay.WithOnBeforeRequest(func(ctx context.Context, req *relay.Request) error {
        req.WithHeader("X-Request-ID", uuid.New().String())
        return nil
    }),
    relay.WithOnAfterResponse(func(ctx context.Context, resp *relay.Response) error {
        metrics.RecordLatency(resp.Timing.Total)
        return nil
    }),
)
```

Hooks receive `req.Method()`, `req.URL()`, and `req.Tag(key)` for routing decisions
without needing to inspect the raw `*http.Request`.

---

## Extension Ecosystem

### Observability & Monitoring

#### OpenTelemetry Tracing (`ext/tracing`)

Automatic W3C TraceContext propagation, span creation, and HTTP attribute recording:

```go
import relaytracing "github.com/jhonsferg/relay/ext/tracing"

client := relay.New(
    relaytracing.WithTracing(tracerProvider, propagator),
)
```

#### OpenTelemetry Metrics (`ext/metrics`)

Records `request_count`, `request_duration_ms`, and `active_requests`:

```go
import relaymetrics "github.com/jhonsferg/relay/ext/metrics"

client := relay.New(
    relaymetrics.WithOTelMetrics(meterProvider),
)
```

#### Prometheus (`ext/prometheus`)

Native Prometheus histograms and counters without the OTel SDK:

```go
import relayprom "github.com/jhonsferg/relay/ext/prometheus"

client := relay.New(
    relayprom.WithPrometheus(prometheus.DefaultRegisterer, "myapp"),
)
```

#### Sentry (`ext/sentry`)

Capture network failures and 5xx responses as Sentry events with full HTTP context:

```go
import relaysentry "github.com/jhonsferg/relay/ext/sentry"

client := relay.New(
    relaysentry.WithSentry(sentry.CurrentHub()),
    relaysentry.WithCaptureClientErrors(true), // also capture 4xx
)
```

---

### Caching Backends

#### Redis (`ext/redis`)

Share cached responses across multiple service instances:

```go
import relayredis "github.com/jhonsferg/relay/ext/redis"

store := relayredis.NewCacheStore(redisClient, "relay:cache:")
client := relay.New(relay.WithCache(store))
```

Entries are serialized as JSON. `Clear()` uses `SCAN + DEL` with the key prefix,
never `FLUSHDB`.

#### Memcached (`ext/memcached`)

```go
import relaymemcached "github.com/jhonsferg/relay/ext/memcached"

store := relaymemcached.NewCacheStore(memcacheClient, "relay:")
client := relay.New(relay.WithCache(store))
```

#### In-Memory LRU Cache (`ext/cache/lru`)

Zero-dependency in-process cache with O(1) eviction:

```go
import relaycachelru "github.com/jhonsferg/relay/ext/cache/lru"

store := relaycachelru.New(1000) // capacity: 1000 entries
client := relay.New(relay.WithCache(store))
```

#### Two-Level Cache (`ext/cache/twolevel`)

Combine a fast L1 (e.g. LRU) with a persistent L2 (e.g. Redis). L1 misses that hit
L2 are automatically backfilled into L1:

```go
import (
    relaycachelru "github.com/jhonsferg/relay/ext/cache/lru"
    relaytwolevel "github.com/jhonsferg/relay/ext/cache/twolevel"
)

l1 := relaycachelru.New(500)
l2 := relayredis.NewCacheStore(rdb, "relay:")

store := relaytwolevel.New(l1, l2)
client := relay.New(relay.WithCache(store))
```

---

### Security & Cloud

#### OAuth 2.0 Client Credentials (`ext/oauth`)

Automatic token fetch and transparent background refresh for M2M auth:

```go
import relayoauth "github.com/jhonsferg/relay/ext/oauth"

client := relay.New(
    relayoauth.WithClientCredentials(relayoauth.Config{
        TokenURL:     "https://auth.example.com/oauth/token",
        ClientID:     os.Getenv("CLIENT_ID"),
        ClientSecret: os.Getenv("CLIENT_SECRET"),
        Scopes:       []string{"api:read", "api:write"},
    }),
)
```

#### AWS Signature V4 (`ext/sigv4`)

Sign requests for any AWS service (S3, DynamoDB, API Gateway, …):

```go
import relaysigv4 "github.com/jhonsferg/relay/ext/sigv4"

client := relay.New(
    relaysigv4.WithSigV4(relaysigv4.Config{
        Region:  "us-east-1",
        Service: "execute-api",
    }),
)
```

#### gRPC-Gateway Metadata (`ext/grpc`)

Bridge relay clients to gRPC-Gateway proxies by adding `Grpc-Metadata-*` headers
without importing any gRPC or protobuf packages:

```go
import relaygrpc "github.com/jhonsferg/relay/ext/grpc"

client := relay.New(
    relay.WithBaseURL("https://grpc-gateway.example.com"),
    relaygrpc.WithMetadata("x-tenant-id", tenantID),
    relaygrpc.WithTimeoutHeader(), // forwards context deadline as Grpc-Timeout
)

// Per-request binary metadata (base64-encoded, -Bin suffix)
req := relaygrpc.SetBinaryMetadata("x-signature", sigBytes)(client.Post("/v1/orders"))
```

Parse metadata echoed back in responses:

```go
meta, err := relaygrpc.ParseMetadata(resp.Header)
```

---

### Resilience

#### Advanced Backoff Strategies (`ext/jitterbug`)

Drop-in replacement for the built-in exponential backoff:

```go
import relayjitter "github.com/jhonsferg/relay/ext/jitterbug"

client := relay.New(
    relay.WithRetry(&relay.RetryConfig{
        MaxAttempts: 5,
        Backoff:     relayjitter.NewDecorrelatedJitter(100*time.Millisecond, 30*time.Second),
    }),
)
```

Available strategies: `DecorrelatedJitter`, `LinearBackoff`, retry budget.

#### sony/gobreaker Circuit Breaker (`ext/breaker/gobreaker`)

Plug in the battle-tested [gobreaker](https://github.com/sony/gobreaker) library:

```go
import relaybreaker "github.com/jhonsferg/relay/ext/breaker/gobreaker"

cb := relaybreaker.NewCircuitBreaker(gobreaker.Settings{
    Name:        "payments-api",
    MaxRequests: 3,
    Interval:    10 * time.Second,
    Timeout:     30 * time.Second,
})

client := relay.New(
    relaybreaker.WithGoBreaker(cb),
)
```

HTTP 5xx responses are counted as failures; the response is still returned to the
caller so your application can decide how to handle it.

#### Distributed Rate Limiter (`ext/ratelimit/distributed`)

Redis sliding-window rate limiter with atomic Lua script — safe across multiple
service replicas:

```go
import relaydist "github.com/jhonsferg/relay/ext/ratelimit/distributed"

limiter := relaydist.New(
    redisClient,
    "my-service:rate",   // Redis key prefix
    100,                 // max requests
    time.Minute,         // window
)

client := relay.New(
    relaydist.WithRateLimit(limiter),
)
```

Fails open on Redis errors to avoid taking down your service when the rate limiter
itself is unavailable.

---

### Logging

All adapters implement the same `relay.Logger` interface:

```go
type Logger interface {
    Debug(msg string, args ...any)
    Info(msg string, args ...any)
    Warn(msg string, args ...any)
    Error(msg string, args ...any)
}
```

#### go.uber.org/zap (`ext/zap`)

```go
import relayzap "github.com/jhonsferg/relay/ext/zap"

client := relay.New(
    relay.WithLogger(relayzap.NewAdapter(zapLogger)),
    // or: relayzap.NewSugaredAdapter(sugar)
)
```

#### rs/zerolog (`ext/zerolog`)

```go
import relayzl "github.com/jhonsferg/relay/ext/zerolog"

client := relay.New(
    relay.WithLogger(relayzl.NewAdapter(zerologLogger)),
)
```

#### sirupsen/logrus (`ext/logrus`)

```go
import relaylogrus "github.com/jhonsferg/relay/ext/logrus"

client := relay.New(
    relay.WithLogger(relaylogrus.NewAdapter(logrus.StandardLogger())),
    // or: relaylogrus.NewEntryAdapter(entry) for pre-set fields
)
```

---

### OpenAPI Validation (`ext/openapi`)

Validate every request — and optionally every response — against an OpenAPI 3.x spec
before it reaches the network. Route mismatches are passed through (the server will 404).

```go
import relayopenapi "github.com/jhonsferg/relay/ext/openapi"

doc, err := relayopenapi.LoadFile("openapi.yaml")
if err != nil {
    log.Fatal(err)
}

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relayopenapi.WithValidation(doc,
        relayopenapi.WithResponseValidation(), // also validate responses
        relayopenapi.WithStrict(),             // reject unknown query params/headers
    ),
)

// Check for validation errors
if _, err := client.Execute(req); err != nil {
    if ve, ok := relayopenapi.IsValidationError(err); ok {
        log.Printf("OpenAPI %s validation failed: %v", ve.Phase, ve.Cause)
    }
}
```

---

### Compression

#### Brotli (`ext/brotli`)

Transparent `br` decompression — advertises `Accept-Encoding: br` and decompresses
the response body automatically:

```go
import relaybr "github.com/jhonsferg/relay/ext/brotli"

client := relay.New(relaybr.WithBrotliDecompression())
```

---

## Testing

### testutil — Mock HTTP Server

The built-in `testutil` package provides a mock HTTP server for unit tests without
needing to set up a real server:

```go
import "github.com/jhonsferg/relay/testutil"

func TestMyAPI(t *testing.T) {
    srv := testutil.NewMockServer()
    defer srv.Close()

    srv.Enqueue(testutil.MockResponse{
        Status: 200,
        Body:   `{"status":"ok"}`,
    })

    client := relay.New(relay.WithBaseURL(srv.URL()))
    resp, _ := client.Execute(client.Get("/health"))

    req, _ := srv.TakeRequest(time.Second)
    if req.URL.Path != "/health" {
        t.Errorf("unexpected path: %s", req.URL.Path)
    }
}
```

### ext/mock — Programmable Transport

For unit tests that should never touch the network, `ext/mock` intercepts all
requests inside the process:

```go
import relaymock "github.com/jhonsferg/relay/ext/mock"

mt := relaymock.New(t).
    On(relaymock.GET("/users/1")).Respond(200, `{"id":1,"name":"Alice"}`).
    On(relaymock.POST("/users")).RespondSeq(
        relaymock.Seq(201, `{"id":2}`, nil),
        relaymock.Seq(409, `{"error":"duplicate"}`, nil),
    ).
    Default(relaymock.Respond(503, `{"error":"unavailable"}`))

client := relay.New(relaymock.WithMock(mt))

// Make requests — no network calls are made
resp, _ := client.Execute(client.Get("/users/1"))
mt.AssertExpectations() // verify all rules were hit
```

Rules support exact URL, method, path prefix, and custom predicate matchers.

---

## Examples

The `examples/` directory contains runnable programs demonstrating every feature:

| Directory | What it shows |
|-----------|--------------|
| `examples/basic/` | Simple GET/POST, query params, path params, JSON decode |
| `examples/retry/` | Exponential backoff, custom retry predicate |
| `examples/circuit_breaker/` | Trip → open → reset cycle |
| `examples/healthcheck/` | Automatic circuit breaker recovery via health probe |
| `examples/dns_cache/` | DNS result caching with concurrency and TTL demo |
| `examples/sse/` | Server-Sent Events streaming with multi-line data |
| `examples/jsonl_stream/` | JSONL/NDJSON streaming with `ExecuteAsStream[T]` |
| `examples/streaming/` | Raw response body streaming |
| `examples/tls_pinning/` | Certificate pinning (correct/wrong/rotation) |
| `examples/digest_auth/` | HTTP Digest Authentication challenge/response |
| `examples/progress/` | Upload and download progress bars |
| `examples/coalescing/` | Request deduplication — hit counter shows upstream savings |
| `examples/async/` | `ExecuteAsync`, fan-out, first-to-respond, map-reduce |
| `examples/middleware/` | Transport middleware chain, `OnBeforeRequest`, `OnAfterResponse` |
| `examples/batch/` | Batch execution with `ExecuteBatch` |
| `examples/redis/` | Redis-backed cache with miss/hit/TTL/Clear |
| `examples/redis_cache/` | Cache invalidation and conditional requests |
| `examples/otel/` | OpenTelemetry end-to-end (traces + metrics) |
| `examples/prometheus/` | Prometheus scrape endpoint wiring |
| `examples/zap_logger/` | go.uber.org/zap adapter usage |
| `examples/zerolog_logger/` | rs/zerolog adapter usage |
| `examples/oauth2/` | OAuth 2.0 Client Credentials flow |
| `examples/har_recording/` | HAR 1.2 traffic capture and export |

Run any example:

```bash
cd examples/sse && go run .
```

---

## Performance

Relay is built for high-throughput services:

- **Zero-allocation pooling** — `sync.Pool` for internal buffers keeps GC pressure low.
- **Request coalescing** — collapses identical concurrent requests into a single upstream call, eliminating thundering-herd on cache warm-up.
- **Optimized transport** — pre-tuned connection pool with keep-alive and native HTTP/2 support.
- **Lazy body sizing** — response bodies are read into a capped buffer; oversized responses are rejected early without allocating.
- **DNS caching** — optional client-side DNS cache eliminates repeated resolver round-trips for long-lived services.

---

## Contributing

Contributions are welcome. Please open an issue first to discuss significant changes.

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Commit your changes: `git commit -m 'feat: add my feature'`
4. Push and open a Pull Request

---

<div align="center">

Distributed under the MIT License. See [LICENSE](LICENSE) for details.

Built with care by [jhonsferg](https://github.com/jhonsferg)

</div>
