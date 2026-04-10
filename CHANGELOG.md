# Changelog

All notable changes to relay will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/)
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- CI: benchmarks removed from the automated `Test` pipeline matrix; benchmarks now run on-demand via the separate `Benchmark` workflow. This reduces pipeline duration significantly and avoids false failures caused by infrastructure timing variance.

## [0.3.24] - 2026-04-10

### Fixed

- `fix(hedging,har)`: return abandoned speculative responses to pool; cap HAR response body capture to 1 MB to prevent memory exhaustion on large downloads.
- `fix(request)`: sanitise multipart `FieldName` and `FileName` against header injection attacks (v0.3.23).
- `fix(srv)`: release mutex before DNS lookup in `SRVResolver.Resolve` to prevent deadlock under concurrent service discovery (v0.3.22).
- `fix(sse)`: SSE reconnection never fired after connection drop; `SSEFanOut` discarded its derived context causing subscribers to receive no events after a restart (v0.3.21).
- `fix(ext/vcr)`: return response on cassette save failure instead of swallowing it; fix unchecked `Close` that could mask body read errors (v0.3.20).
- `fix(ext/otel)`: redact URL credentials in span attributes; migrate to semconv v1.26 stable attribute keys; correct HTTP status code attribute type from string to int (v0.3.19).
- `fix(ext/compress)`: replace streaming codec API with `EncodeAll`/`DecodeAll` to eliminate data race during concurrent compression (v0.3.18).
- `fix(ext/websocket)`: implement full RFC 6455 close handshake in `CloseGracefully`; previous version sent a close frame but did not wait for the peer's close frame (v0.3.17).
- `fix(ext/vcr)`: close response body before returning `ReadAll` error in VCR playback path (v0.3.16).
- `fix(ext/oauth,ext/sigv4)`: credential value leaked into error message text; remove dead `ctx != nil` check that was always true (v0.3.15).
- `fix(ext/chaos,ext/mock,ext/oidc)`: three correctness bugs — chaos middleware applied wrong fault type, mock recorder double-counted headers, OIDC token cache eviction skipped expired entries (v0.3.14).
- `fix(ext/otel)`: bump OTel SDK from v1.34.0 to v1.40.0; resolves CVE GHSA-hfvc-g4fc-pqhx in `go.opentelemetry.io/otel/sdk` (v0.3.13).
- `fix(ext/memcached)`: SHA-256 key hashing for URLs longer than 250 bytes to comply with Memcached key length limit; fix flaky benchmark caused by goroutine scheduling variation (v0.3.12).

## [0.3.11] - 2026-04-09

### Fixed

- `fix(core)`: four correctness bugs — nil pointer panic in `ExecuteBatch` on empty slice, memory leak in `WithRequestCoalescing` cache map, invalid regexp in URL sanitiser, zero-alloc regression in header builder.
- `fix(bulkhead)`: eliminate slot-stealing race in priority queue release path; high-priority waiters could acquire a slot intended for an already-released goroutine (v0.3.10).
- `fix(tracing,http3)`: correct OTel span attribute type for HTTP status code; add managed transport teardown for `ext/http3` to prevent goroutine leak on client shutdown (v0.3.9).
- `fix(slog,jitterbug)`: `ext/slog` lost the request context during log emission; `ext/jitterbug` did not reuse keep-alive connections due to a missing `DisableKeepAlives: false` reset (v0.3.8).
- `fix(compress,oauth)`: `ext/compress` had an unprotected map write under concurrent requests; `ext/oauth` token store had a lost-update window during refresh (v0.3.7).
- `fix(ext/prometheus,ext/vcr)`: `ext/prometheus` panicked on duplicate metric registration; `ext/vcr` playback returned incorrect status text on redirect responses (v0.3.6).
- `fix(retry)`: skip retry for non-idempotent methods (`POST`, `PATCH`) by default; opt-in via `RetryConfig.RetryOnNonIdempotent = true` (v0.3.5).
- `fix(request)`: normalise double leading slashes in RFC 3986 host-only base URL paths (v0.3.4 / v0.3.3).

## [0.3.0] - 2026-04-08

### Added

- `feat(client)`: `Client.BaseURL()` accessor returns the configured base URL string, useful inside `OnBeforeRequest` hooks for routing decisions (v0.3.0).

### Fixed

- `fix(security)`: sanitise credential and default headers to prevent injection via `WithDefaultHeader`; fix heap corruption when reusing request buffers with large bodies; validate load-balancer backend URLs on construction; cap HAR body capture (v0.3.2).
- `fix(security)`: harden proxy URL handling against SSRF; reject header values containing CRLF; restrict `HTTPError.Body` size to prevent unbounded buffer growth; fix circuit breaker state machine race on concurrent half-open probes (v0.3.1).
- `fix(url)`: handle trailing-slash base URLs correctly in `ResolveReference`; fix `%3F` double-encoding when query strings are appended (v0.2.3).

## [0.2.0] - 2026-04-07

### Added

- `feat(cmd/relay-gen)`: OpenAPI 3.x → type-safe Go client code generator; reads a JSON or YAML OpenAPI specification and emits a `relay`-based client with typed request/response structs and one method per operation.
- `feat(core)`: HTTP/2 server push promise handler via `WithHTTP2PushHandler(fn)`.
- `feat(core)`: WASM/`js` build support — core package now compiles with `GOOS=js GOARCH=wasm`; Unix socket transport excluded via build tags.
- `feat(core)`: HAR 1.2 export for request/response tracing via `WithHARRecording`; `HARRecorder.All()` range-over-func iterator.
- `feat(core)`: Response schema validation middleware via `WithSchemaValidation`; validates response bodies against a provided JSON Schema before returning them to the caller.
- `feat(core)`: DNS SRV service discovery via `WithSRVDiscovery(service, proto)`; resolves `_service._proto.domain` records and round-robins across returned backends.
- `feat(credentials)`: credential rotation hooks — `WithCredentialRotation(provider, refreshInterval)` swaps credentials in the live client without restart.
- `feat(compress)`: dictionary-based zstd compression in `ext/compress` via `WithZstdDict(dict)`; reduces payload size for repetitive API responses.
- `feat(core)`: `MultiSigner` chains multiple `RequestSigner` implementations applied in order per attempt.
- `feat(core)`: `HMACRequestSigner` signs requests with HMAC-SHA256 over `"METHOD URL UNIX-TIMESTAMP"`, injecting `X-Signature` / `X-Timestamp` headers.
- `feat(core)`: `WithRequestSigner(s)` alias for `WithSigner` added for naming consistency.
- `feat(transport)`: Unix domain socket transport via `WithUnixSocket(path)`; routes all requests through a socket while preserving the HTTP host header.
- `feat(sse)`: `SSEFanOut` multiplexer broadcasts a single upstream SSE stream to multiple downstream subscribers with independent buffering.
- `feat(ext/prometheus)`: request duration and size histograms.
- `feat(ext/otel)`: native OpenTelemetry tracing and metrics extension (`ext/otel`) as a standalone module.
- `feat(ext/oidc)`: OIDC/JWT bearer token provider extension.
- CI: benchmark regression detection for PRs via `benchstat`; benchmark dashboard published to GitHub Pages.
- CI: per-extension auto-tagging and auto-release workflows.

### Changed

- Go 1.25 opt-in improvements: adopted new standard library APIs where available while maintaining 1.24 minimum.
- `chore(core)`: API stability review; added `doc.go` with stability guarantees.

### Removed

- `ext/logrus` — removed after deprecation period; migrate to `ext/slog`.
- `ext/zerolog` — removed after deprecation period; migrate to `ext/slog`.

### Fixed

- `fix(test)`: added timing delay in concurrent deduplication tests to prevent race condition under Go 1.25 scheduler.
- `fix(ci)`: merged autotag and autorelease into a single workflow.

## [0.1.11] - 2026-04-07

### Removed

- `ext/logrus`  -  removed after deprecation period; migrate to `ext/slog` (see `docs/migration/logrus-zerolog-to-slog.md`)
- `ext/zerolog`  -  removed after deprecation period; migrate to `ext/slog` (see `docs/migration/logrus-zerolog-to-slog.md`)

### Added

- `WithUnixSocket(path)` - routes all requests through a Unix domain socket while preserving the HTTP host header; enables Docker/containerd socket APIs without a TCP listener.
- `MultiSigner` - chains multiple `RequestSigner` implementations applied in order per attempt; `NewMultiSigner(signers...)` filters nil entries.
- `HMACRequestSigner` - signs requests with HMAC-SHA256 over `"METHOD URL UNIX-TIMESTAMP"` and injects `X-Signature` / `X-Timestamp` headers; safe for concurrent use.
- `WithRequestSigner(s)` - alias for `WithSigner` added for naming consistency with the signer-related API surface.
- `relay-gen` (`cmd/relay-gen`) - OpenAPI 3.x → type-safe Go client code generator; reads a JSON spec and emits a `relay`-based client with typed request/response structs and one method per operation.
- `WithHTTP2PushHandler(fn)` - registers a `PushPromiseHandler` callback invoked for HTTP/2 server-pushed responses; handler receives the pushed URL and response (body must be closed by handler).
- `HARRecorder.All() iter.Seq[HAREntry]` - range-over-func iterator (Go 1.23+ `iter` package) providing a snapshot-based loop over recorded entries without holding the lock.
- WASM/js build support: core package now builds with `GOOS=js GOARCH=wasm`; Unix socket transport excluded via build tags.
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
