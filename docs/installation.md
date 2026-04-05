# Installation

This page covers everything you need to get relay up and running in your Go project, from requirements through verifying your setup.

## Requirements

relay requires **Go 1.24 or later**. It uses generics (introduced in Go 1.18) and takes advantage of the improved `net/http` toolchain stabilised in Go 1.24. No C dependencies are needed - relay is pure Go.

To check your Go version:

```bash
go version
```

You should see output similar to:

```
go version go1.24.0 linux/amd64
```

If your version is older, download the latest Go toolchain from [https://go.dev/dl](https://go.dev/dl).

## Installing the core module

Add relay to your project with a single `go get` command:

```bash
go get github.com/jhonsferg/relay
```

This fetches the core module and updates your `go.mod` and `go.sum` files automatically.

### What the core module includes

The core module (`github.com/jhonsferg/relay`) ships with everything you need for production HTTP work without pulling in any heavyweight third-party dependencies:

- HTTP/1.1 and HTTP/2 support via the standard library transport
- Retry with exponential backoff and jitter
- Circuit breaker (sliding-window failure rate)
- Bulkhead (bounded concurrency semaphore)
- Request hedging
- Rate limiting (token bucket)
- Bearer, Basic, API Key, Digest, and HMAC authentication
- Semantic hook points (BeforeRequest, AfterResponse, OnError, BeforeRetry, BeforeRedirect)
- First-class WebSocket upgrade
- Dynamic TLS certificate reloading
- Generic JSON/XML decode helpers
- Built-in test utilities (mock transport and VCR recorder)

## go.mod example

After running `go get`, your `go.mod` will look similar to this:

```
module github.com/your-org/your-app

go 1.24

require (
    github.com/jhonsferg/relay v1.0.0
)
```

Pin a specific version when you need reproducible builds:

```bash
go get github.com/jhonsferg/relay@v1.0.0
```

> **Note:** relay follows semantic versioning. Patch releases (`v1.0.x`) are always backward compatible. Minor releases (`v1.x.0`) may add new opt-in features. Major releases (`vX`) introduce breaking changes.

## Extension modules

relay ships 18 optional extension modules under the `ext/` subtree. Each extension is a separate Go module so you only pay the dependency cost for what you actually use.

Install any extension with `go get` just like the core:

```bash
go get github.com/jhonsferg/relay/ext/tracing
```

Then register the extension with your client at startup:

```go
package main

import (
    "github.com/jhonsferg/relay"
    "github.com/jhonsferg/relay/ext/tracing"
)

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        tracing.WithOpenTelemetry(), // inject the extension
    )
    _ = client
}
```

### All 18 extension modules

| Module | Import path | Purpose |
|--------|-------------|---------|
| tracing | `github.com/jhonsferg/relay/ext/tracing` | OpenTelemetry distributed tracing |
| metrics | `github.com/jhonsferg/relay/ext/metrics` | OpenTelemetry metrics (histograms, counters) |
| prometheus | `github.com/jhonsferg/relay/ext/prometheus` | Prometheus metrics exporter |
| sentry | `github.com/jhonsferg/relay/ext/sentry` | Sentry error and performance tracking |
| http3 | `github.com/jhonsferg/relay/ext/http3` | HTTP/3 QUIC transport via quic-go |
| websocket | `github.com/jhonsferg/relay/ext/websocket` | Legacy WebSocket helpers (use core WebSocket API for new code) |
| grpc | `github.com/jhonsferg/relay/ext/grpc` | gRPC bridge - call gRPC services through the relay client interface |
| graphql | `github.com/jhonsferg/relay/ext/graphql` | GraphQL query builder and response helpers |
| oauth | `github.com/jhonsferg/relay/ext/oauth` | OAuth2 flows - authorization code, client credentials, device flow |
| cache | `github.com/jhonsferg/relay/ext/cache` | Redis-backed HTTP response caching with RFC 7234 semantics |
| mock | `github.com/jhonsferg/relay/ext/mock` | In-process mock transport for unit and integration tests |
| sigv4 | `github.com/jhonsferg/relay/ext/sigv4` | AWS SigV4 request signing for AWS service APIs |
| openapi | `github.com/jhonsferg/relay/ext/openapi` | OpenAPI 3.x request/response validation |
| brotli | `github.com/jhonsferg/relay/ext/brotli` | Brotli compression/decompression transport wrapper |
| zap | `github.com/jhonsferg/relay/ext/zap` | Uber Zap structured logger integration |
| zerolog | `github.com/jhonsferg/relay/ext/zerolog` | rs/zerolog structured logger integration |
| logrus | `github.com/jhonsferg/relay/ext/logrus` | Logrus structured logger integration |
| jitterbug | `github.com/jhonsferg/relay/ext/jitterbug` | Configurable jitter strategies for retry and polling intervals |

#### tracing - OpenTelemetry tracing

```bash
go get github.com/jhonsferg/relay/ext/tracing
```

Instruments every request with an OpenTelemetry span. Propagates W3C `traceparent` and `tracestate` headers automatically. Compatible with any OTel-compliant backend (Jaeger, Tempo, Honeycomb, Datadog).

#### metrics - OpenTelemetry metrics

```bash
go get github.com/jhonsferg/relay/ext/metrics
```

Records request duration, error rate, and in-flight request count using the OpenTelemetry Metrics API. Export to any OTel-compatible backend via the configured `MeterProvider`.

#### prometheus - Prometheus metrics

```bash
go get github.com/jhonsferg/relay/ext/prometheus
```

Registers Prometheus histograms and counters for request duration, status codes, and retry attempts. Integrates with the default `prometheus.DefaultRegisterer` or a custom registry.

#### sentry - Sentry error tracking

```bash
go get github.com/jhonsferg/relay/ext/sentry
```

Captures HTTP errors and panics to Sentry. Attaches request URL, method, and status code as breadcrumbs. Respects the Sentry sampling rate configured in your DSN.

#### http3 - HTTP/3 QUIC transport

```bash
go get github.com/jhonsferg/relay/ext/http3
```

Replaces the default transport with a QUIC-based HTTP/3 transport backed by `quic-go`. Falls back to HTTP/2 automatically when the server does not advertise QUIC via `Alt-Svc`.

#### grpc - gRPC bridge

```bash
go get github.com/jhonsferg/relay/ext/grpc
```

Exposes a relay-compatible interface over gRPC channels. Useful when you want uniform retry, circuit-breaker, and observability behaviour across both REST and gRPC service calls.

#### graphql - GraphQL helpers

```bash
go get github.com/jhonsferg/relay/ext/graphql
```

Provides a `Query` builder for constructing GraphQL requests and a `DecodeGraphQL` helper that surfaces both data and errors from a standard GraphQL response envelope.

#### oauth - OAuth2 flows

```bash
go get github.com/jhonsferg/relay/ext/oauth
```

Handles OAuth2 token acquisition and refresh transparently. Supports authorization code (with PKCE), client credentials, and device authorization flows. Tokens are cached in memory with automatic pre-emptive refresh before expiry.

#### cache - Redis response caching

```bash
go get github.com/jhonsferg/relay/ext/cache
```

Caches GET responses in Redis using RFC 7234 cache semantics. Respects `Cache-Control`, `Expires`, `ETag`, and `Last-Modified` headers. Useful for dramatically reducing load on upstream APIs with stable data.

#### mock - Mock transport

```bash
go get github.com/jhonsferg/relay/ext/mock
```

Provides an in-process mock transport for testing. Define expected requests and canned responses without starting a real HTTP server. Also includes a VCR-style cassette recorder for replay-based testing.

#### sigv4 - AWS SigV4 signing

```bash
go get github.com/jhonsferg/relay/ext/sigv4
```

Signs every request with AWS Signature Version 4 for authenticating to AWS services (API Gateway, S3, DynamoDB, etc.). Reads credentials from the standard AWS credential chain.

#### openapi - OpenAPI validation

```bash
go get github.com/jhonsferg/relay/ext/openapi
```

Validates outgoing requests and incoming responses against an OpenAPI 3.x specification at runtime. Returns structured validation errors so you can surface schema violations early in development.

#### brotli - Brotli compression

```bash
go get github.com/jhonsferg/relay/ext/brotli
```

Adds a transport wrapper that advertises `br` in the `Accept-Encoding` header and transparently decompresses Brotli-encoded responses, complementing the standard gzip/deflate support.

#### zap - Zap logger

```bash
go get github.com/jhonsferg/relay/ext/zap
```

Routes relay's internal log events (request start, retry, circuit-break trip, error) to a `*zap.Logger`. Preserves structured fields for correlation with application logs.

#### zerolog - Zerolog logger

```bash
go get github.com/jhonsferg/relay/ext/zerolog
```

Routes relay log events to a `zerolog.Logger`. Emits structured JSON log lines with the request method, URL, status, duration, and attempt number on every event.

#### logrus - Logrus logger

```bash
go get github.com/jhonsferg/relay/ext/logrus
```

Routes relay log events to a `*logrus.Logger`. Each event includes standard logrus fields compatible with Logstash, Datadog Logs, and other log aggregation pipelines.

#### jitterbug - Jitter utilities

```bash
go get github.com/jhonsferg/relay/ext/jitterbug
```

Provides pluggable jitter strategies - full jitter, decorrelated jitter, equal jitter - for use with the retry subsystem. Useful when the default jitter strategy does not match your service's backoff requirements.

## Installing multiple extensions at once

You can install several extensions in one command:

```bash
go get \
    github.com/jhonsferg/relay/ext/tracing \
    github.com/jhonsferg/relay/ext/prometheus \
    github.com/jhonsferg/relay/ext/zap
```

After this, your `go.mod` will contain entries for each module:

```
require (
    github.com/jhonsferg/relay v1.0.0
    github.com/jhonsferg/relay/ext/tracing v1.0.0
    github.com/jhonsferg/relay/ext/prometheus v1.0.0
    github.com/jhonsferg/relay/ext/zap v1.0.0
)
```

> **Tip:** Extensions are versioned in lock-step with the core module. When you upgrade relay, run `go get github.com/jhonsferg/relay/...@latest` to upgrade all installed extensions at once.

## Verifying the installation

Create a file called `hello_relay.go` in your project root:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

// Post represents a single post from the JSONPlaceholder test API.
type Post struct {
    UserID int    `json:"userId"`
    ID     int    `json:"id"`
    Title  string `json:"title"`
    Body   string `json:"body"`
}

func main() {
    // Build a client with a base URL and a 5-second default timeout.
    client := relay.New(
        relay.WithBaseURL("https://jsonplaceholder.typicode.com"),
        relay.WithTimeout(5_000), // milliseconds
    )

    // Build a GET request for post #1.
    req := client.Get("/posts/1")

    // Execute the request using a background context.
    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        log.Fatalf("request failed: %v", err)
    }

    // Decode the JSON response body into a Post struct.
    post, err := relay.DecodeJSON[Post](resp)
    if err != nil {
        log.Fatalf("decode failed: %v", err)
    }

    fmt.Printf("Post #%d by user %d: %s\n", post.ID, post.UserID, post.Title)
}
```

Run it:

```bash
go run hello_relay.go
```

Expected output (title text may differ as the API is public):

```
Post #1 by user 1: sunt aut facere repellat provident occaecati excepturi optio reprehenderit
```

If you see a post title printed, relay is installed correctly and able to make outbound HTTPS requests.

> **Warning:** The JSONPlaceholder API used in this verification example is a public read-only test service. Do not send sensitive data to it. For production integration testing, use the built-in mock transport from `github.com/jhonsferg/relay/ext/mock`.

## Upgrading

To upgrade relay and all installed extensions to the latest release:

```bash
go get github.com/jhonsferg/relay@latest
# Upgrade any extensions you have installed:
go get github.com/jhonsferg/relay/ext/tracing@latest
go get github.com/jhonsferg/relay/ext/prometheus@latest
# ... repeat for other extensions
```

Then tidy your module graph:

```bash
go mod tidy
```

## Uninstalling

Remove relay from your project by deleting all import references to it, then run:

```bash
go mod tidy
```

`go mod tidy` will remove any modules from `go.mod` and `go.sum` that are no longer referenced by your code.
