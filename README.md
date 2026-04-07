<div align="center">

# Relay

**A production-grade, declarative HTTP client for Go.**

[![Go Version](https://img.shields.io/badge/Go-1.24%2B-00ADD8?style=for-the-badge&logo=go)](https://pkg.go.dev/github.com/jhonsferg/relay)
[![CI](https://img.shields.io/github/actions/workflow/status/jhonsferg/relay/ci.yml?style=for-the-badge&logo=github&label=CI)](https://github.com/jhonsferg/relay/actions/workflows/ci.yml)
[![Tests](https://img.shields.io/badge/tests-6%20OS%2FGo%20combos-0099ff?style=for-the-badge&logo=github)](https://github.com/jhonsferg/relay/actions/workflows/ci.yml)
[![Codecov](https://img.shields.io/badge/coverage-tracked-41B883?style=for-the-badge&logo=codecov)](https://codecov.io/gh/jhonsferg/relay)
[![CodeQL](https://img.shields.io/github/actions/workflow/status/jhonsferg/relay/codeql.yml?style=for-the-badge&logo=github&label=CodeQL)](https://github.com/jhonsferg/relay/actions/workflows/codeql.yml)
[![Trivy](https://img.shields.io/badge/vulnerability%20scan-Trivy-1f77b4?style=for-the-badge&logo=github)](https://github.com/jhonsferg/relay/actions/workflows/trivy.yml)
[![Release](https://img.shields.io/github/v/release/jhonsferg/relay?style=for-the-badge&logo=github&color=orange)](https://github.com/jhonsferg/relay/releases/latest)
[![pkg.go.dev](https://img.shields.io/badge/pkg.go.dev-reference-007D9C?style=for-the-badge&logo=go)](https://pkg.go.dev/github.com/jhonsferg/relay)
[![Go Report Card](https://img.shields.io/badge/go%20report-A%2B-brightgreen?style=for-the-badge)](https://goreportcard.com/report/github.com/jhonsferg/relay)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg?style=for-the-badge)](LICENSE)

---

**[Documentation](https://jhonsferg.github.io/relay) · [pkg.go.dev](https://pkg.go.dev/github.com/jhonsferg/relay) · [Quick Start](#quick-start) · [Extensions](#extensions) · [Tools](#tools) · [Changelog](CHANGELOG.md)**

</div>

## Overview

Relay brings the ergonomics of Python's *requests* and the resilience of *Resilience4j* to Go. It provides a fluent, batteries-included API for building resilient HTTP clients: retries, circuit breaking, rate limiting, deduplication, adaptive timeouts, load balancing, and full observability  -  all composable via options.

The core module has **zero external dependencies**. Every integration (Redis, OTel, Prometheus, gRPC, slog, chaos, VCR, etc.) lives in its own optional extension module so you only pull in what you need.

```bash
go get github.com/jhonsferg/relay
```

Requires Go 1.24 or later. WASM (`js/wasm`) targets are supported.

---

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

type Repo struct {
    ID   int    `json:"id"`
    Name string `json:"full_name"`
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.github.com"),
        relay.WithRetry(relay.RetryConfig{MaxAttempts: 3}),
        relay.WithTimeout(10*time.Second),
    )

    var repo Repo
    _, err := relay.Get[Repo](context.Background(), client, "/repos/golang/go", &repo)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(repo.Name)
}
```

---

## Features

### Core

| Feature | Description |
|---------|-------------|
| **Fluent request builder** | Chain `.GET()`, `.POST()`, `.Header()`, `.Query()`, `.Body()` |
| **Retry & backoff** | Exponential backoff with jitter, configurable retry conditions |
| **Circuit breaker** | Automatic open/half-open/closed state machine |
| **Rate limiting** | Token bucket and sliding window algorithms |
| **Request deduplication** | In-flight singleflight to collapse concurrent identical requests |
| **Retry budget** | Sliding window budget to prevent retry storms |
| **Client-side load balancer** | Round-robin and random strategies across backends |
| **Adaptive timeout** | Percentile-based timeout derived from observed latency |
| **Bulkhead isolation** | Concurrency limits per client or endpoint group |
| **Request hedging** | Parallel speculative requests, use first response |
| **Streaming** | `io.Reader` and channel-based streaming for large payloads |
| **Hooks** | `OnBeforeRequest` / `OnAfterResponse` for auth, logging, metrics |
| **Generic decode** | `relay.Get[T]`, `relay.Post[T]` with zero-alloc JSON decoding |
| **Error classification** | Distinguish transient / permanent / rate-limited errors |
| **ETag & idempotency** | Built-in idempotency key generation and ETag support |
| **TLS & certificates** | Dynamic cert reloading, mTLS, custom CA bundles |

### Auth & Credentials

| Feature | Description |
|---------|-------------|
| **Bearer / Basic auth** | `WithBearerToken`, `WithBasicAuth` options |
| **HMAC-SHA256 signing** | `HMACRequestSigner` sets `X-Timestamp` + `X-Signature` automatically |
| **Multi-signer** | `NewMultiSigner` chains multiple signers in order |
| **Credential rotation** | `RotatingTokenProvider` refreshes tokens before expiry with a configurable threshold |
| **Custom signer** | Implement the `RequestSigner` interface for any auth scheme |
| **mTLS** | Mutual TLS with client certificates |

### Transport

| Feature | Description |
|---------|-------------|
| **Unix domain socket** | `WithUnixSocket`  -  connect to local services via socket path (Linux/macOS) |
| **DNS SRV discovery** | `WithSRVDiscovery`  -  resolve service endpoints via DNS SRV records with TTL caching |
| **HTTP/2 push promises** | `WithHTTP2PushHandler`  -  handle server push promises and cache pushed responses |
| **WASM/js** | Builds on `js/wasm`; `WithUnixSocket` is a no-op on that target for portability |

### Compression

| Feature | Description |
|---------|-------------|
| **Gzip / Zstd** | `WithCompression(relay.Gzip)` or `WithCompression(relay.Zstd)` for response decompression |
| **Request compression** | `WithRequestCompression` compresses outgoing request bodies |
| **Dictionary Zstd** | `ext/compress`  -  `ZstdDictionaryCompressor` for pre-shared dictionary compression |

### Observability

| Feature | Description |
|---------|-------------|
| **HAR export** | `HARRecorder` captures all traffic in HAR format; `HARRecorder.All()` returns a Go 1.23 `iter.Seq[HAREntry]` iterator |
| **OpenTelemetry** | `ext/otel`  -  unified tracing + metrics via `WithOtel(tracer, meter)` |
| **Prometheus** | `ext/prometheus`  -  Prometheus metrics exporter |

### Validation

| Feature | Description |
|---------|-------------|
| **Response schema validation** | `WithSchemaValidator`  -  validate decoded responses against struct tags or a JSON Schema |
| **Struct validator** | `NewStructValidator`  -  validates required fields and rules via struct tags |
| **JSON Schema validator** | `NewJSONSchemaValidator`  -  validates against an inline JSON Schema definition |

> Full feature documentation: **[jhonsferg.github.io/relay](https://jhonsferg.github.io/relay)**

---

## Extensions

Each extension is a standalone Go module  -  add only what you use:

| Module | Import path | Description |
|--------|-------------|-------------|
| `ext/compress` | `github.com/jhonsferg/relay/ext/compress` | Dictionary-based Zstd compression (`ZstdDictionaryCompressor`) |
| `ext/oidc` | `github.com/jhonsferg/relay/ext/oidc` | OIDC/JWT bearer token provider |
| `ext/otel` | `github.com/jhonsferg/relay/ext/otel` | OpenTelemetry tracing + metrics in one option (`WithOtel`) |
| `ext/tracing` | `github.com/jhonsferg/relay/ext/tracing` | OpenTelemetry distributed tracing (standalone) |
| `ext/metrics` | `github.com/jhonsferg/relay/ext/metrics` | OpenTelemetry metrics (standalone) |
| `ext/prometheus` | `github.com/jhonsferg/relay/ext/prometheus` | Prometheus metrics exporter |
| `ext/slog` | `github.com/jhonsferg/relay/ext/slog` | Structured logging via `log/slog` |
| `ext/zap` | `github.com/jhonsferg/relay/ext/zap` | Zap logging integration |
| `ext/chaos` | `github.com/jhonsferg/relay/ext/chaos` | Fault injection for resilience testing |
| `ext/vcr` | `github.com/jhonsferg/relay/ext/vcr` | HTTP cassette recording and playback |
| `ext/mock` | `github.com/jhonsferg/relay/ext/mock` | Mock transport for unit tests |
| `ext/oauth` | `github.com/jhonsferg/relay/ext/oauth` | OAuth2 token management |
| `ext/sigv4` | `github.com/jhonsferg/relay/ext/sigv4` | AWS SigV4 request signing |
| `ext/openapi` | `github.com/jhonsferg/relay/ext/openapi` | OpenAPI request/response validation |
| `ext/redis` | `github.com/jhonsferg/relay/ext/redis` | Redis-backed cache and rate limiting |
| `ext/http3` | `github.com/jhonsferg/relay/ext/http3` | HTTP/3 QUIC transport |
| `ext/websocket` | `github.com/jhonsferg/relay/ext/websocket` | WebSocket upgrade |
| `ext/grpc` | `github.com/jhonsferg/relay/ext/grpc` | gRPC bridge transport |
| `ext/graphql` | `github.com/jhonsferg/relay/ext/graphql` | GraphQL query support |
| `ext/sentry` | `github.com/jhonsferg/relay/ext/sentry` | Sentry error reporting |
| `ext/brotli` | `github.com/jhonsferg/relay/ext/brotli` | Brotli compression support |
| `ext/breaker/gobreaker` | `github.com/jhonsferg/relay/ext/breaker/gobreaker` | Circuit breaker backed by `gobreaker` |
| `ext/cache/lru` | `github.com/jhonsferg/relay/ext/cache/lru` | In-process LRU response cache |
| `ext/cache/twolevel` | `github.com/jhonsferg/relay/ext/cache/twolevel` | Two-level (L1+L2) response cache |
| `ext/ratelimit/distributed` | `github.com/jhonsferg/relay/ext/ratelimit/distributed` | Distributed rate limiting |
| `ext/memcached` | `github.com/jhonsferg/relay/ext/memcached` | Memcached-backed cache |
| `ext/jitterbug` | `github.com/jhonsferg/relay/ext/jitterbug` | Pluggable jitter strategies for retry backoff |

> Extension documentation: **[jhonsferg.github.io/relay/extensions](https://jhonsferg.github.io/relay/extensions/index/)**

---

## Tools

### relay-gen  -  OpenAPI client generator

`relay-gen` reads an OpenAPI 3.x spec and generates a type-safe Go client using relay:

```bash
go install github.com/jhonsferg/relay/cmd/relay-gen@latest

relay-gen -spec openapi.json -pkg acme -out ./acme/client.go
```

The generated client exposes one method per operation with strongly-typed request/response structs and full relay middleware support.

### relay-probe  -  health check CLI

```bash
go install github.com/jhonsferg/relay/cmd/relay-probe@latest
relay-probe https://api.example.com/health
```

### relay-bench  -  micro-benchmarking harness

```bash
go install github.com/jhonsferg/relay/cmd/relay-bench@latest
relay-bench -url https://api.example.com/ping -n 1000
```

---

## Unix Socket Transport

Connect to services exposed via Unix domain sockets (Linux/macOS):

```go
client := relay.New(
    relay.WithBaseURL("http://localhost"),
    relay.WithUnixSocket("/var/run/myapp.sock"),
)
resp, err := client.Execute(relay.NewRequest().GET("/status"))
```

> **Note:** On `js/wasm` targets `WithUnixSocket` is accepted but silently ignored, keeping call sites portable.

---

## DNS SRV Discovery

Resolve backends dynamically from DNS SRV records:

```go
resolver := relay.NewSRVResolver("myservice", "tcp", "example.com", "https",
    relay.WithSRVTTL(30*time.Second),
    relay.WithSRVBalancer(relay.SRVRoundRobin),
)
client := relay.New(relay.WithSRVDiscovery(resolver))
```

---

## HTTP/2 Push Promises

Handle server-pushed resources and cache them for subsequent requests:

```go
cache := relay.NewPushedResponseCache()
client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relay.WithHTTP2PushHandler(cache),
)
```

---

## HAR Recording

Capture all traffic in [HAR](https://w3c.github.io/web-performance/specs/HAR/Overview.html) format for debugging or test fixtures:

```go
rec := relay.NewHARRecorder()
client := relay.New(relay.WithMiddleware(rec.Middleware()))

// iterate with a Go 1.23 range-over-func loop
for entry := range rec.All() {
    fmt.Println(entry.Request.URL, entry.Response.Status)
}

data, _ := rec.Export()
os.WriteFile("traffic.har", data, 0o644)
```

---

## Request Authentication

**HMAC-SHA256 signing**  -  sets `X-Timestamp` and `X-Signature` headers automatically:

```go
client := relay.New(
    relay.WithRequestSigner(&relay.HMACRequestSigner{Key: []byte(secret)}),
)
```

**Rotating token provider**  -  refreshes credentials before expiry:

```go
provider := relay.NewRotatingTokenProvider(fetchTokenFunc, 5*time.Minute)
client := relay.New(relay.WithCredentialProvider(provider))
```

**Multiple signers**  -  chain signers in order with `NewMultiSigner`:

```go
client := relay.New(
    relay.WithRequestSigner(relay.NewMultiSigner(
        &relay.HMACRequestSigner{Key: signingKey},
        relay.RequestSignerFunc(func(r *http.Request) error {
            r.Header.Set("X-Tenant", tenantID)
            return nil
        }),
    )),
)
```

---

## Response Schema Validation

Validate decoded responses against struct tags or a JSON Schema:

```go
// struct-tag validation
validator := relay.NewStructValidator(MyResponse{})
client := relay.New(relay.WithSchemaValidator(validator))

// JSON Schema validation
validator, err := relay.NewJSONSchemaValidator(`{"type":"object","required":["id"]}`)
client := relay.New(relay.WithSchemaValidator(validator))
```

---

## CI/CD

Relay's CI pipeline runs across 6 OS/Go version combinations and includes:

- **Unit & integration tests**  -  `ci.yml`
- **Benchmark regression detection**  -  `benchstat.yml` compares PR benchmarks against the base branch using [benchstat](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) and fails the build if a statistically significant slowdown is detected
- **CodeQL static analysis**  -  `codeql.yml`
- **Vulnerability scanning**  -  Trivy (`trivy.yml`)

---

## Testing

```go
import "github.com/jhonsferg/relay/testutil"

srv := testutil.NewMockServer()
defer srv.Close()

srv.Enqueue(testutil.Response{Status: 200, Body: `{"id":1}`})

client := relay.New(relay.WithBaseURL(srv.URL))
resp, err := client.Execute(relay.NewRequest().GET("/items/1"))
```

> Testing guide: **[jhonsferg.github.io/relay/guides/testing](https://jhonsferg.github.io/relay/guides/testing/)**

---

## Documentation

The full documentation is at **[jhonsferg.github.io/relay](https://jhonsferg.github.io/relay)**:

- [Getting Started](https://jhonsferg.github.io/relay/quickstart/)
- [All Features](https://jhonsferg.github.io/relay/features/)
- [Extension Modules](https://jhonsferg.github.io/relay/extensions/)
- [API Reference](https://pkg.go.dev/github.com/jhonsferg/relay) on pkg.go.dev
- 📊 **[Live Benchmark Dashboard](https://jhonsferg.github.io/relay/benchmarks/)**

---

## License

MIT - see [LICENSE](LICENSE).
