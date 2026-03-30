# Changelog

All notable changes to relay will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.4.0] - 2026-03-29

### Added

- `examples/zap_logger/` — demonstrates `ext/zap` with development logger, named sub-loggers, level filtering, and `NewSugaredAdapter`
- `examples/zerolog_logger/` — demonstrates `ext/zerolog` with `ConsoleWriter`, context fields, level filtering, and JSON production logger
- `examples/redis/` — self-contained Redis cache demo using miniredis; covers cache miss/hit, TTL expiry, forced revalidation, `Delete`/`Clear`, prefix isolation, and multiple clients sharing one store
- `examples/prometheus/` — Prometheus metrics extension with custom registry, label inspection, `promhttp.HandlerFor` wiring
- `examples/har_recording/` — HAR 1.2 capture with `Entries()` / `Export()` / `Reset()` and shared-recorder pattern
- Examples table added to README covering all 13 example directories

## [0.3.0] - 2026-03-29

### Added

- `ext/zap` module (`github.com/jhonsferg/relay/ext/zap`) — go.uber.org/zap adapter implementing `relay.Logger` via `NewAdapter(*zap.Logger)` and `NewSugaredAdapter(*zap.SugaredLogger)`
- `ext/zerolog` module (`github.com/jhonsferg/relay/ext/zerolog`) — github.com/rs/zerolog adapter implementing `relay.Logger` via `NewAdapter(zerolog.Logger)`; forwards key/value pairs using zerolog's `event.Fields()`
- `ext/redis` module (`github.com/jhonsferg/relay/ext/redis`) — Redis-backed `relay.CacheStore` via `NewCacheStore(redis.Cmdable, prefix)` using github.com/redis/go-redis/v9; entries serialised as compact JSON with per-entry TTL derived from `ExpiresAt`; `Clear()` uses `SCAN + DEL` with the key prefix to avoid `FLUSHDB` on shared instances

## [0.2.0] - 2026-03-29

### Added

- `ext/oauth` module (`github.com/jhonsferg/relay/ext/oauth`) — OAuth 2.0 Client Credentials with auto-refresh as a standalone module
- `ext/tracing` module (`github.com/jhonsferg/relay/ext/tracing`) — OpenTelemetry distributed tracing as a standalone module
- `ext/metrics` module (`github.com/jhonsferg/relay/ext/metrics`) — OpenTelemetry metrics (request count, duration, active requests) as a standalone module
- `ext/prometheus` module (`github.com/jhonsferg/relay/ext/prometheus`) — Prometheus metrics adapter as a standalone module
- `ext/brotli` module (`github.com/jhonsferg/relay/ext/brotli`) — Transparent `br` decompression + `Accept-Encoding: br` injection as a standalone module
- `go.work` workspace for local multi-module development
- Full test suites for all five extension modules
- Minimum Go version raised to **1.22** (from 1.21)

### Changed

- **BREAKING:** `WithOAuth2ClientCredentials` moved from core to `github.com/jhonsferg/relay/ext/oauth` — import `relayoauth "github.com/jhonsferg/relay/ext/oauth"` and use `relayoauth.WithClientCredentials`
- **BREAKING:** `WithTracing` moved from core to `github.com/jhonsferg/relay/ext/tracing` — import `relaytracing "github.com/jhonsferg/relay/ext/tracing"` and use `relaytracing.WithTracing`
- **BREAKING:** `WithMetrics` / `WithOTelMetrics` moved from core to `github.com/jhonsferg/relay/ext/metrics` — import `relaymetrics "github.com/jhonsferg/relay/ext/metrics"` and use `relaymetrics.WithOTelMetrics`
- **BREAKING:** Prometheus integration moved from core (build tag `prometheus`) to `github.com/jhonsferg/relay/ext/prometheus` — import `relayprom "github.com/jhonsferg/relay/ext/prometheus"` and use `relayprom.WithPrometheus`
- **BREAKING:** Brotli decompression moved from core (build tag `brotli`) to `github.com/jhonsferg/relay/ext/brotli` — import `relaybrotli "github.com/jhonsferg/relay/ext/brotli"` and use `relaybrotli.WithBrotliDecompression`
- Core module now depends only on the Go standard library and `golang.org/x/sync`

### Removed

- Build tags `brotli` and `prometheus` — replaced by separate `ext/` modules

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
