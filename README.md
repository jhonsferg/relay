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

**[Documentation](https://jhonsferg.github.io/relay) - [pkg.go.dev](https://pkg.go.dev/github.com/jhonsferg/relay) - [Quick Start](#quick-start) - [Extensions](#extensions) - [Changelog](CHANGELOG.md)**

</div>

## Overview

Relay brings the ergonomics of Python's *requests* and the resilience of *Resilience4j* to Go. It provides a fluent, batteries-included API for building resilient HTTP clients: retries, circuit breaking, rate limiting, deduplication, adaptive timeouts, load balancing, and full observability - all composable via options.

The core module has **zero external dependencies**. Every integration (Redis, OTel, Prometheus, gRPC, slog, chaos, VCR, etc.) lives in its own optional extension module so you only pull in what you need.

```bash
go get github.com/jhonsferg/relay
```

Requires Go 1.24 or later.

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
    client := relay.NewClient(
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

## Core Features

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

> Full feature documentation: **[jhonsferg.github.io/relay](https://jhonsferg.github.io/relay)**

---

## Extensions

Each extension is a standalone Go module - add only what you use:

| Module | Import path | Description |
|--------|-------------|-------------|
| `ext/chaos` | `github.com/jhonsferg/relay/ext/chaos` | Fault injection for resilience testing |
| `ext/slog` | `github.com/jhonsferg/relay/ext/slog` | Structured logging via `log/slog` |
| `ext/vcr` | `github.com/jhonsferg/relay/ext/vcr` | HTTP cassette recording and playback |
| `ext/tracing` | `github.com/jhonsferg/relay/ext/tracing` | OpenTelemetry distributed tracing |
| `ext/metrics` | `github.com/jhonsferg/relay/ext/metrics` | OpenTelemetry metrics |
| `ext/prometheus` | `github.com/jhonsferg/relay/ext/prometheus` | Prometheus metrics exporter |
| `ext/sentry` | `github.com/jhonsferg/relay/ext/sentry` | Sentry error reporting |
| `ext/http3` | `github.com/jhonsferg/relay/ext/http3` | HTTP/3 QUIC transport |
| `ext/websocket` | `github.com/jhonsferg/relay/ext/websocket` | WebSocket upgrade |
| `ext/grpc` | `github.com/jhonsferg/relay/ext/grpc` | gRPC bridge transport |
| `ext/graphql` | `github.com/jhonsferg/relay/ext/graphql` | GraphQL query support |
| `ext/oauth` | `github.com/jhonsferg/relay/ext/oauth` | OAuth2 token management |
| `ext/sigv4` | `github.com/jhonsferg/relay/ext/sigv4` | AWS SigV4 request signing |
| `ext/openapi` | `github.com/jhonsferg/relay/ext/openapi` | OpenAPI request validation |
| `ext/redis` | `github.com/jhonsferg/relay/ext/redis` | Redis-backed cache and rate limiting |
| `ext/mock` | `github.com/jhonsferg/relay/ext/mock` | Mock transport for unit tests |
| `ext/logrus` | `github.com/jhonsferg/relay/ext/logrus` | Logrus logging integration |
| `ext/zerolog` | `github.com/jhonsferg/relay/ext/zerolog` | Zerolog logging integration |
| `ext/zap` | `github.com/jhonsferg/relay/ext/zap` | Zap logging integration |
| `ext/brotli` | `github.com/jhonsferg/relay/ext/brotli` | Brotli compression support |

> Extension documentation: **[jhonsferg.github.io/relay/extensions](https://jhonsferg.github.io/relay/extensions/index/)**

---

## Testing

```go
import "github.com/jhonsferg/relay/testutil"

// Spin up an in-process mock server
srv := testutil.NewMockServer()
defer srv.Close()

srv.Enqueue(testutil.Response{Status: 200, Body: `{"id":1}`})

client := relay.NewClient(relay.WithBaseURL(srv.URL))
resp, err := client.Execute(ctx, relay.NewRequest().GET("/items/1"))
```

> Testing guide: **[jhonsferg.github.io/relay/guides/testing](https://jhonsferg.github.io/relay/guides/testing/)**

---

## Documentation

The full documentation is at **[jhonsferg.github.io/relay](https://jhonsferg.github.io/relay)**:

- [Getting Started](https://jhonsferg.github.io/relay/quickstart/)
- [All Features](https://jhonsferg.github.io/relay/features/)
- [Extension Modules](https://jhonsferg.github.io/relay/extensions/)
- [API Reference](https://pkg.go.dev/github.com/jhonsferg/relay) on pkg.go.dev

---

## License

MIT - see [LICENSE](LICENSE).
