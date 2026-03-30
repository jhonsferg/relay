# Changelog

All notable changes to relay will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.1] - 2026-03-30 (ext/tracing, ext/metrics)

### Fixed

- `ext/tracing`: updated `require github.com/jhonsferg/relay` to `v0.1.1` - fixes broken dependency chain from proxy-cached `v0.2.0` that referenced `relay@v0.1.0`
- `ext/metrics`: updated `require github.com/jhonsferg/relay` to `v0.1.1` - same fix as above

## [0.1.1] - 2026-03-30

### Fixed

- Core `go.mod` now contains only `golang.org/x/sync` as a direct dependency; removes the 11 ext-module `require` lines that `go work sync` incorrectly injected into the original `v0.1.0` release, which were cached permanently by the Go module proxy

## [0.2.0] - 2026-03-30 (ext/tracing, ext/metrics)

### Added

- `ext/tracing`: `WithInstrumentationName(name string) Option` - sets the OTel instrumentation scope name passed to `tp.Tracer()`. Empty string falls back to the default `"github.com/jhonsferg/relay"`.
- `ext/tracing`: `WithInstrumentationVersion(version string) Option` - attaches an instrumentation scope version to every span (e.g. `"1.0.0"`).
- `ext/metrics`: `WithInstrumentationName(name string) Option` - sets the OTel instrumentation scope name passed to `mp.Meter()`. Empty string falls back to the default `"github.com/jhonsferg/relay"`.
- `ext/metrics`: `WithInstrumentationVersion(version string) Option` - attaches an instrumentation scope version to every metric instrument.

Both `WithTracing` and `WithOTelMetrics` now accept variadic `...Option` parameters, remaining fully backwards-compatible with existing call sites.

```go
// tracing - custom scope
relaytracing.WithTracing(tp, prop,
    relaytracing.WithInstrumentationName("my-service"),
    relaytracing.WithInstrumentationVersion("1.0.0"),
)

// metrics - custom scope
relaymetrics.WithOTelMetrics(mp,
    relaymetrics.WithInstrumentationName("my-service"),
    relaymetrics.WithInstrumentationVersion("1.0.0"),
)
```

## [0.6.0] - 2026-03-30

### Added

#### New extension modules

- `ext/mock` (`github.com/jhonsferg/relay/ext/mock`) - in-process mock transport for unit tests; define `Rule`-based matchers (exact URL, method, path prefix, custom predicate) with fixed or sequential responses; `WithMock` replaces the transport entirely, no network calls are made
- `ext/logrus` (`github.com/jhonsferg/relay/ext/logrus`) - github.com/sirupsen/logrus adapter implementing `relay.Logger`; `NewAdapter(*logrus.Logger)` and `NewEntryAdapter(*logrus.Entry)` forward alternating key/value pairs as `logrus.Fields`
- `ext/cache/lru` (`github.com/jhonsferg/relay/ext/cache/lru`) - in-memory LRU cache implementing `relay.CacheStore` using `container/list` for O(1) eviction; configurable capacity
- `ext/cache/twolevel` (`github.com/jhonsferg/relay/ext/cache/twolevel`) - two-level cache (L1 fast + L2 persistent) implementing `relay.CacheStore`; L1 misses that hit L2 are backfilled into L1 automatically
- `ext/breaker/gobreaker` (`github.com/jhonsferg/relay/ext/breaker/gobreaker`) - github.com/sony/gobreaker circuit breaker integration; `WithGoBreaker(cb)` transport middleware counts HTTP 5xx responses as failures while still returning the response to the caller
- `ext/ratelimit/distributed` (`github.com/jhonsferg/relay/ext/ratelimit/distributed`) - Redis sliding-window distributed rate limiter via atomic Lua script; `New(redis, key, limit, window)` + `WithRateLimit(limiter)` relay option; fail-open on Redis errors
- `ext/openapi` (`github.com/jhonsferg/relay/ext/openapi`) - OpenAPI 3.x request and optional response validation via github.com/getkin/kin-openapi; `LoadFile`/`Load` parse specs, `WithValidation(doc)` installs transport middleware, unknown routes are passed through; `WithResponseValidation()` and `WithStrict()` options
- `ext/grpc` (`github.com/jhonsferg/relay/ext/grpc`) - gRPC-Gateway metadata bridge; `WithMetadata(key, value)` attaches `Grpc-Metadata-*` headers to every request, `WithBinaryMetadata` base64-encodes binary values with `-Bin` suffix, `WithTimeoutHeader()` forwards context deadline as `Grpc-Timeout`, `ParseMetadata` extracts metadata from response headers

## [0.5.0] - 2026-03-30

### Added

- `Request.Clone()` - deep-copies a request builder including headers, query params, path params, tags, and body bytes; mutations to the clone do not affect the original
- `Request.WithMaxBodySize(n int64)` - per-request override for `Config.MaxResponseBodyBytes`; pass `-1` to disable the limit for a single request without changing the client default
- `WithHealthCheck(url, interval, timeout, expectedStatus)` - starts a background goroutine that probes a health endpoint while the circuit breaker is Open and resets it automatically on a healthy response, bypassing the full `ResetTimeout`
- `WithDNSCache(ttl)` - client-side DNS result cache; each unique hostname is resolved at most once per TTL interval, reducing lookup latency and resolver load on high-concurrency workloads
- `ExecuteSSE(req, handler)` - typed Server-Sent Events consumer; parses `id`, `event`, `data`, and `retry` fields; handler returns `false` to stop the stream early
- `relay.ExecuteAsStream[T](client, req, handler)` - generic JSONL/NDJSON streaming decoder; decodes each newline-delimited JSON line into `T` and calls handler lazily without buffering the full body
- `SSEEvent` type with `ID`, `Event`, `Data`, and `Retry` fields
- `SSEHandler` type alias `func(SSEEvent) bool`
- `examples/sse/` - SSE streaming demo with multi-line data, early stop, and per-request timeout
- `examples/jsonl_stream/` - JSONL streaming demo including OpenAI-style chat delta accumulation and `Clone()`-based request reuse
- `examples/healthcheck/` - health check probe demo showing automatic circuit breaker recovery
- `examples/dns_cache/` - DNS cache demo with concurrency test and TTL expiry

### Changed

- `Client.Shutdown` now cancels all background goroutines (health check) before draining in-flight requests; adding `bgCancel context.CancelFunc` to `Client`
- Added `Request.Method()` and `Request.URL()` read-only accessors (useful inside `OnBeforeRequest` hooks for logging and routing)

### Examples

- `examples/tls_pinning/` - certificate pinning with correct pin, wrong pin, multi-pin rotation, and production pattern
- `examples/digest_auth/` - HTTP Digest Authentication with challenge/response, multiple requests, wrong credentials, and combining with retry/logger
- `examples/progress/` - download progress bar, upload progress, multipart file upload progress, combined upload+download
- `examples/coalescing/` - request deduplication demo showing upstream hit counts with/without coalescing, POST bypass, auth-header isolation, coalescing+cache combination
- `examples/async/` - `ExecuteAsync` fan-out, first-to-respond wins, `ExecuteAsyncCallback` success/error, context-scoped cancellation, map-reduce aggregation
- `examples/middleware/` - `WithTransportMiddleware` chain (timing + request-ID), `WithOnBeforeRequest` (dynamic tokens, logging, maintenance block), `WithOnAfterResponse` (validation, error promotion), middleware ordering diagram, `Client.With` per-operation variants

## [0.4.0] - 2026-03-29

### Added

- `examples/zap_logger/` - demonstrates `ext/zap` with development logger, named sub-loggers, level filtering, and `NewSugaredAdapter`
- `examples/zerolog_logger/` - demonstrates `ext/zerolog` with `ConsoleWriter`, context fields, level filtering, and JSON production logger
- `examples/redis/` - self-contained Redis cache demo using miniredis; covers cache miss/hit, TTL expiry, forced revalidation, `Delete`/`Clear`, prefix isolation, and multiple clients sharing one store
- `examples/prometheus/` - Prometheus metrics extension with custom registry, label inspection, `promhttp.HandlerFor` wiring
- `examples/har_recording/` - HAR 1.2 capture with `Entries()` / `Export()` / `Reset()` and shared-recorder pattern
- Examples table added to README covering all 13 example directories

## [0.3.0] - 2026-03-29

### Added

- `ext/zap` module (`github.com/jhonsferg/relay/ext/zap`) - go.uber.org/zap adapter implementing `relay.Logger` via `NewAdapter(*zap.Logger)` and `NewSugaredAdapter(*zap.SugaredLogger)`
- `ext/zerolog` module (`github.com/jhonsferg/relay/ext/zerolog`) - github.com/rs/zerolog adapter implementing `relay.Logger` via `NewAdapter(zerolog.Logger)`; forwards key/value pairs using zerolog's `event.Fields()`
- `ext/redis` module (`github.com/jhonsferg/relay/ext/redis`) - Redis-backed `relay.CacheStore` via `NewCacheStore(redis.Cmdable, prefix)` using github.com/redis/go-redis/v9; entries serialized as compact JSON with per-entry TTL derived from `ExpiresAt`; `Clear()` uses `SCAN + DEL` with the key prefix to avoid `FLUSHDB` on shared instances

## [0.2.0] - 2026-03-29

### Added

- `ext/oauth` module (`github.com/jhonsferg/relay/ext/oauth`) - OAuth 2.0 Client Credentials with auto-refresh as a standalone module
- `ext/tracing` module (`github.com/jhonsferg/relay/ext/tracing`) - OpenTelemetry distributed tracing as a standalone module
- `ext/metrics` module (`github.com/jhonsferg/relay/ext/metrics`) - OpenTelemetry metrics (request count, duration, active requests) as a standalone module
- `ext/prometheus` module (`github.com/jhonsferg/relay/ext/prometheus`) - Prometheus metrics adapter as a standalone module
- `ext/brotli` module (`github.com/jhonsferg/relay/ext/brotli`) - Transparent `br` decompression + `Accept-Encoding: br` injection as a standalone module
- `go.work` workspace for local multi-module development
- Full test suites for all five extension modules
- Minimum Go version raised to **1.22** (from 1.21)

### Changed

- **BREAKING:** `WithOAuth2ClientCredentials` moved from core to `github.com/jhonsferg/relay/ext/oauth` - import `relayoauth "github.com/jhonsferg/relay/ext/oauth"` and use `relayoauth.WithClientCredentials`
- **BREAKING:** `WithTracing` moved from core to `github.com/jhonsferg/relay/ext/tracing` - import `relaytracing "github.com/jhonsferg/relay/ext/tracing"` and use `relaytracing.WithTracing`
- **BREAKING:** `WithMetrics` / `WithOTelMetrics` moved from core to `github.com/jhonsferg/relay/ext/metrics` - import `relaymetrics "github.com/jhonsferg/relay/ext/metrics"` and use `relaymetrics.WithOTelMetrics`
- **BREAKING:** Prometheus integration moved from core (build tag `prometheus`) to `github.com/jhonsferg/relay/ext/prometheus` - import `relayprom "github.com/jhonsferg/relay/ext/prometheus"` and use `relayprom.WithPrometheus`
- **BREAKING:** Brotli decompression moved from core (build tag `brotli`) to `github.com/jhonsferg/relay/ext/brotli` - import `relaybrotli "github.com/jhonsferg/relay/ext/brotli"` and use `relaybrotli.WithBrotliDecompression`
- Core module now depends only on the Go standard library and `golang.org/x/sync`

### Removed

- Build tags `brotli` and `prometheus` - replaced by separate `ext/` modules

## [0.1.0] - 2026-03-29

### Added

- Initial public release
- Declarative fluent request builder (`Get`, `Post`, `Put`, `Patch`, `Delete`, `Head`, `Options`)
- Connection pool tuning, HTTP/2, TLS 1.2+ enforcement
- Retry with exponential backoff + full jitter (`RetryConfig`)
- Circuit breaker pattern: Closed → Open → Half-Open (`CircuitBreakerConfig`)
- Client-side token-bucket rate limiting (`WithRateLimit`)
- RFC 7234 HTTP caching with pluggable backend (`CacheStore`, `WithInMemoryCache`)
- Streaming support: `ExecuteStream` for SSE, JSONL, large downloads
- Async execution: `ExecuteAsync` (channel) and `ExecuteAsyncCallback`
- Batch concurrent fan-out: `ExecuteBatch` with bounded concurrency
- OpenTelemetry distributed tracing and metrics (`WithTracing`, `WithMetrics`)
- Pluggable `RoundTripper` middleware chain (`WithTransportMiddleware`)
- `OnBeforeRequest` / `OnAfterResponse` hooks
- Graceful shutdown with in-flight drain (`Shutdown`)
- Error classification: transient, permanent, rate-limited, canceled
- `Client.With` for deriving variants with different settings
- Structured logger interface with slog adapter (`WithLogger`, `SlogAdapter`)
- OAuth 2.0 Client Credentials flow with auto-refresh (`WithOAuth2ClientCredentials`)
- TLS certificate pinning with SHA-256 (`WithCertificatePinning`)
- Upload/download progress callbacks (`WithUploadProgress`, `WithDownloadProgress`)
- Request coalescing/deduplication for GET/HEAD (`WithRequestCoalescing`)
- HTTP Digest Authentication RFC 7616 (`WithDigestAuth`)
- HAR 1.2 recording and export (`WithHARRecording`, `HARRecorder`)
- Request timing breakdown: DNS, TCP, TLS, TTFB, content transfer (`Response.Timing`)
- Auto idempotency key injection on retries (`WithAutoIdempotencyKey`, `WithIdempotencyKey`)
- Generic `ExecuteAs[T]` helper for typed JSON deserialization
- `testutil` package: `MockServer` and `RequestRecorder`
- `internal/backoff`: reusable exponential backoff with full jitter
- `internal/pool`: byte buffer pool to reduce GC pressure
