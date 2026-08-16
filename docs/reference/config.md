# Configuration Reference

All relay client configuration is applied through functional option functions passed to `relay.New` (or `(*Client).With` to derive a new client from an existing one). Each function returns a `relay.Option` value. Options are applied left-to-right; later options win when they touch the same field.

This reference lists every `With*` option function exported by the `relay` package (the core module - not `ext/*` extensions), in alphabetical order. A short note on scope: some names below look like they might be per-request helpers - they are not. Per-request concerns (a one-off `Authorization` header, a request-scoped timeout, path/query params) are set via methods on `*relay.Request` (e.g. `req.WithBearerToken(...)`, `req.WithBasicAuth(...)`, `req.WithAPIKey(...)`, `req.WithTimeout(...)`), returned by `client.Get`/`client.Post`/etc. - not via `relay.With*` options passed to `relay.New`. This file only covers client-construction options.

---

## WithAdaptiveTimeout

```go
func WithAdaptiveTimeout(cfg relay.AdaptiveTimeoutConfig) relay.Option
```

Enables adaptive per-request timeouts computed from a percentile of recently observed response latencies, instead of a single fixed `WithTimeout` value. Automatically forces timing instrumentation on (as if `WithTiming` were also passed), since adaptive timeout needs latency samples to function.

**relay.AdaptiveTimeoutConfig fields:**

| Field | Type | Description | Default if zero |
|-------|------|-------------|------|
| `Percentile` | `float64` | Latency percentile used as the base (e.g. `0.95` for p95). | 0.95 |
| `Multiplier` | `float64` | Scales the percentile latency to get the timeout (e.g. `2.0` = 2× p95). | 2.0 |
| `WindowSize` | `int` | Number of recent latency observations kept. | 100 |
| `MinTimeout` | `time.Duration` | Floor on the computed timeout. | 100ms |
| `MaxTimeout` | `time.Duration` | Ceiling on the computed timeout. | 30s |
| `InitialTimeout` | `time.Duration` | Used until enough observations accumulate. | 5s |

**Default:** Disabled; every request uses the fixed `WithTimeout` value.

```go
client := relay.New(relay.WithAdaptiveTimeout(relay.AdaptiveTimeoutConfig{
    Percentile: 0.95,
    Multiplier: 2.0,
}))
```

---

## WithAutoIdempotencyKey

```go
func WithAutoIdempotencyKey() relay.Option
```

Automatically injects an `X-Idempotency-Key` UUID v4 header on every request, regardless of HTTP method. The same key is reused across all retry attempts for a given request. Use `WithAutoIdempotencyOnSafeRetries` instead if you only want this for methods that are actually idempotent.

**Default:** Disabled.

```go
client := relay.New(relay.WithAutoIdempotencyKey())
```

---

## WithAutoIdempotencyOnSafeRetries

```go
func WithAutoIdempotencyOnSafeRetries() relay.Option
```

Automatically injects an `X-Idempotency-Key` UUID header on requests whose method is idempotent per RFC 9110 §9.2.2: GET, HEAD, PUT, DELETE, OPTIONS, and TRACE. POST and PATCH are skipped since they are not guaranteed idempotent - use `WithAutoIdempotencyKey` instead if you want the header on every method unconditionally. The key is generated once per original request and reused on all of its retry attempts.

**Default:** Disabled.

> **Note:** The key is injected as soon as the request is sent, regardless of whether it is ever retried - `WithRetry` does not need to be configured for the header to appear.

```go
client := relay.New(
    relay.WithAutoIdempotencyOnSafeRetries(),
    relay.WithRetry(&relay.RetryConfig{MaxAttempts: 3}),
)
```

---

## WithAutoNormaliseURL

```go
func WithAutoNormaliseURL(enable bool) relay.Option
```

Controls whether `WithBaseURL` automatically appends a trailing slash to the base URL when missing. Enabled by default for API convenience; disable it if you need `WithBaseURL` to store the URL exactly as passed, with no modification.

**Default:** `true`.

```go
client := relay.New(
    relay.WithAutoNormaliseURL(false),
    relay.WithBaseURL("https://api.example.com/v1"),
)
```

---

## WithBaseURL

```go
func WithBaseURL(urlStr string) relay.Option
```

Sets the base URL prepended to every request path that does not already start with `http://` or `https://`. The URL is pre-parsed once at construction time for performance. If `WithAutoNormaliseURL` is left at its default (`true`), a trailing slash is added automatically when missing.

**Default:** Empty string; request paths must be absolute URLs.

```go
client := relay.New(relay.WithBaseURL("https://api.example.com/v2"))
```

---

## WithBeforeRedirectHook

```go
func WithBeforeRedirectHook(fn relay.BeforeRedirectHookFunc) relay.Option

type BeforeRedirectHookFunc func(req *http.Request, via []*http.Request) error
```

Appends a hook invoked before each redirect is followed. `via` is the chain of requests followed so far. Returning a non-nil error stops the redirect chain; the error propagates as the `Client.Execute` return value. Multiple hooks may be registered (each call to `WithBeforeRedirectHook` appends one); they run in registration order.

**Default:** No hooks; all redirects within `WithMaxRedirects` are followed automatically.

```go
client := relay.New(relay.WithBeforeRedirectHook(
    func(req *http.Request, via []*http.Request) error {
        log.Printf("redirecting to %s (%d hops so far)", req.URL, len(via))
        return nil
    },
))
```

---

## WithBeforeRetryHook

```go
func WithBeforeRetryHook(fn relay.BeforeRetryHookFunc) relay.Option

type BeforeRetryHookFunc func(ctx context.Context, attempt int, req *relay.Request, httpResp *http.Response, err error)
```

Appends a hook invoked before each retry sleep. `attempt` is 1-based (first retry = 1). `httpResp` and `err` reflect the result that triggered the retry; either may be nil depending on whether the trigger was an HTTP status code or a transport error. Multiple hooks may be registered and run in registration order.

**Default:** No hooks.

```go
client := relay.New(relay.WithBeforeRetryHook(
    func(ctx context.Context, attempt int, req *relay.Request, resp *http.Response, err error) {
        log.Printf("retry #%d for %s: %v", attempt, req.URL(), err)
    },
))
```

---

## WithCache

```go
func WithCache(store relay.CacheStore) relay.Option
```

Attaches a `CacheStore` backend used to cache HTTP responses honoring standard cache-control semantics. Pass `nil` to disable caching. Use `WithInMemoryCache` for the built-in LRU option, or implement the `CacheStore` interface yourself for Redis, disk, or other backends (see `ext/cache/twolevel` and `ext/redis` for examples).

**Default:** `nil`; caching disabled.

```go
client := relay.New(relay.WithCache(myRedisCacheStore))
```

---

## WithCertificatePinning

```go
func WithCertificatePinning(pins []string) relay.Option
```

Rejects TLS connections whose certificate chain does not contain any certificate matching one of the provided SHA-256 pins. Pins must be base64-encoded SHA-256 digests of the certificate's SPKI, optionally prefixed with `"sha256/"`. An invalid pin configuration disables pinning and logs a warning via the configured `Logger` at construction time rather than failing `relay.New`.

**Default:** No pins; standard system trust store validation only.

```go
client := relay.New(relay.WithCertificatePinning([]string{
    "sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
}))
```

---

## WithCertWatcher

```go
func WithCertWatcher(w *relay.CertWatcher) relay.Option
```

Attaches a pre-constructed `*CertWatcher` - there is no standalone exported constructor for one; obtain it from an existing client built with `WithDynamicTLSCert` via `client.Config().CertWatcher`, then share that same watcher with other clients. Unlike `WithDynamicTLSCert`, callers are responsible for starting and stopping the watcher's lifecycle themselves - `Client.Shutdown` does **not** stop a watcher attached this way, since it may be shared across multiple clients.

**Default:** `nil`; no dynamic client certificate.

```go
primary := relay.New(relay.WithDynamicTLSCert("/certs/client.crt", "/certs/client.key", 5*time.Minute))
secondary := relay.New(relay.WithCertWatcher(primary.Config().CertWatcher))
```

---

## WithCircuitBreaker

```go
func WithCircuitBreaker(cbc *relay.CircuitBreakerConfig) relay.Option
```

Replaces the circuit breaker configuration, which trips to `Open` after consecutive failures and stops sending requests to protect a struggling downstream, probing periodically in `HalfOpen` state to detect recovery. Zero/unset fields on a non-nil `cbc` are defaulted individually (you don't have to specify all four).

**relay.CircuitBreakerConfig fields:**

| Field | Type | Description | Default if zero |
|-------|------|-------------|------|
| `MaxFailures` | `int` | Consecutive failures in `Closed` that trip the breaker to `Open`. | 5 |
| `ResetTimeout` | `time.Duration` | How long the breaker stays `Open` before probing again. | 30s (via this option) / 60s (client-wide default) |
| `HalfOpenRequests` | `int` | Max probe requests allowed while `HalfOpen`. | 1 (via this option) / 3 (client-wide default) |
| `SuccessThreshold` | `int` | Consecutive successes in `HalfOpen` required to close the circuit. | 1 (via this option) / 2 (client-wide default) |
| `OnStateChange` | `func(from, to relay.CircuitBreakerState)` | Optional callback on every state transition. | nil |

**Default:** A circuit breaker is always active unless disabled with `WithDisableCircuitBreaker` - the client-wide default is 5 failures, 60s reset, 3 half-open probes, 2-success threshold.

```go
client := relay.New(relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
    MaxFailures:      5,
    ResetTimeout:     30 * time.Second,
    SuccessThreshold: 2,
}))
```

---

## WithClientCert

```go
func WithClientCert(certFile, keyFile string) relay.Option
```

Configures a static TLS client certificate for mutual TLS (mTLS), loaded once from disk at construction time. A load error is silently ignored, leaving the TLS config unchanged - use `WithDynamicTLSCert` instead if hot-reloading or hard-failure-on-error is required.

**Default:** No client certificate presented.

```go
client := relay.New(relay.WithClientCert("/certs/client.crt", "/certs/client.key"))
```

---

## WithClientCertPEM

```go
func WithClientCertPEM(certPEM, keyPEM []byte) relay.Option
```

Same as `WithClientCert`, but loads the certificate/key from PEM-encoded byte slices rather than files - useful when certificates come from an environment variable or a secret manager (Vault, AWS Secrets Manager) rather than disk.

**Default:** No client certificate presented.

```go
client := relay.New(relay.WithClientCertPEM(certPEM, keyPEM))
```

---

## WithCompression

```go
func WithCompression(algo relay.CompressionAlgorithm) relay.Option
```

Enables transparent response decompression. The client advertises the chosen algorithm(s) via `Accept-Encoding` and automatically decompresses responses whose `Content-Encoding` matches. `relay.CompressionAuto` (recommended) advertises `zstd, br, gzip, deflate` and decompresses whichever the server picks; `CompressionZstd`, `CompressionBrotli`, `CompressionGzip` restrict to one algorithm.

**Default:** Not called by default, but response decompression for the standard `Accept-Encoding` negotiated by Go's transport is otherwise on unless `WithDisableCompression` is set - use this option for zstd/brotli support beyond what the stdlib transport handles natively.

```go
client := relay.New(relay.WithCompression(relay.CompressionAuto))
```

---

## WithConnectionPool

```go
func WithConnectionPool(maxIdle, maxIdlePerHost, maxPerHost int) relay.Option
```

Tunes the underlying transport's connection pool. `maxIdle` is the maximum total idle (keep-alive) connections across all hosts; `maxIdlePerHost` per individual host; `maxPerHost` caps total connections (active + idle) per host - `0` means unlimited.

**Default:** `maxIdle=100`, `maxIdlePerHost=20`, `maxPerHost=50`.

```go
client := relay.New(relay.WithConnectionPool(200, 50, 100))
```

---

## WithCookieJar

```go
func WithCookieJar(jar http.CookieJar) relay.Option
```

Sets the cookie jar used by the client for automatic cookie handling across requests. Pass `nil` to disable cookies entirely.

**Default:** A standard RFC 6265 jar (`cookiejar.New(nil)`) is attached automatically.

```go
client := relay.New(relay.WithCookieJar(nil)) // disable cookies
```

---

## WithCredentialProvider

```go
func WithCredentialProvider(p relay.CredentialProvider) relay.Option

type CredentialProvider interface {
    Credentials(ctx context.Context) (relay.Credentials, error)
}
```

Sets a `CredentialProvider` called before each request attempt (including retries) to supply fresh credentials - useful for short-lived tokens (OAuth, Vault) without rebuilding the client. Runs before `WithSigner`. The built-in `relay.RotatingTokenProvider` caches a bearer token and refreshes it only when within a configurable threshold of expiry.

**Default:** `nil`; no credentials applied automatically.

```go
provider := relay.NewRotatingTokenProvider(fetchToken, 30*time.Second)
client := relay.New(relay.WithCredentialProvider(provider))
```

---

## WithCustomDialer

```go
func WithCustomDialer(dialer *net.Dialer) relay.Option
```

Replaces the default `net.Dialer`. When set, `WithDialTimeout`/`WithDialKeepAlive` are ignored in favour of the dialer's own settings.

**Default:** A `net.Dialer` built from `WithDialTimeout`/`WithDialKeepAlive`.

```go
client := relay.New(relay.WithCustomDialer(&net.Dialer{Timeout: 5 * time.Second}))
```

---

## WithDefaultAccept

```go
func WithDefaultAccept(accept string) relay.Option
```

Sets the value sent in the `Accept` header for any request that doesn't already have one explicitly set. Pass `""` to disable the default (Go's HTTP client then sends none, and servers commonly treat that as `*/*`).

**Default:** `""`; no default `Accept` header is added.

```go
client := relay.New(relay.WithDefaultAccept("application/vnd.api+json"))
```

---

## WithDefaultCookieJar

```go
func WithDefaultCookieJar() relay.Option
```

Creates and attaches a fresh standard RFC 6265 cookie jar. Rarely needed explicitly since `relay.New` already attaches one by default - useful mainly to reset back to a jar after an earlier option (or `.With(WithCookieJar(nil))`) cleared it.

**Default:** Already the default; see `WithCookieJar`.

```go
client := relay.New(relay.WithDefaultCookieJar())
```

---

## WithDefaultHeaders

```go
func WithDefaultHeaders(headers map[string]string) relay.Option
```

Merges the given headers into every outgoing request. Per-request headers (set via `Request.WithHeader`) always take precedence over these defaults. Values are sanitised to strip CR/LF characters. Calling this multiple times merges into the existing map rather than replacing it.

**Default:** No default headers.

```go
client := relay.New(relay.WithDefaultHeaders(map[string]string{
    "User-Agent": "my-service/1.0",
}))
```

---

## WithDialKeepAlive

```go
func WithDialKeepAlive(d time.Duration) relay.Option
```

Sets the interval between TCP keep-alive probes sent on active connections. Ignored if `WithCustomDialer` is set.

**Default:** 30 seconds.

```go
client := relay.New(relay.WithDialKeepAlive(15 * time.Second))
```

---

## WithDialTimeout

```go
func WithDialTimeout(d time.Duration) relay.Option
```

Sets the maximum time allowed for a TCP connection to be established. Ignored if `WithCustomDialer` is set.

**Default:** 30 seconds.

```go
client := relay.New(relay.WithDialTimeout(5 * time.Second))
```

---

## WithDigestAuth

```go
func WithDigestAuth(username, password string) relay.Option
```

Enables HTTP Digest Authentication (RFC 7616). The client automatically handles the 401 challenge/response cycle: it sends the initial unauthenticated request, receives the challenge, then retries with the computed digest.

**Default:** Disabled.

```go
client := relay.New(relay.WithDigestAuth("admin", "password123"))
```

---

## WithDisableCircuitBreaker

```go
func WithDisableCircuitBreaker() relay.Option
```

Removes the circuit breaker entirely so all requests are attempted regardless of upstream failure rates. Without this, a client-wide circuit breaker is always active (see `WithCircuitBreaker`'s default).

**Default:** Not disabled; a circuit breaker with conservative defaults is always on unless this option is used.

```go
client := relay.New(relay.WithDisableCircuitBreaker())
```

---

## WithDisableCompression

```go
func WithDisableCompression() relay.Option
```

Disables automatic `Accept-Encoding` negotiation and transparent response decompression at the transport level.

**Default:** Compression negotiation is on.

```go
client := relay.New(relay.WithDisableCompression())
```

---

## WithDisableRedirectTracking

```go
func WithDisableRedirectTracking() relay.Option
```

Skips populating `Response.RedirectCount` and `Response.RedirectChain()`, avoiding a per-request context allocation for callers who don't read either. `WithMaxRedirects` enforcement and `WithBeforeRedirectHook` hooks are unaffected - only the count/chain bookkeeping is skipped.

**Default:** Redirect tracking is on; `RedirectCount`/`RedirectChain()` are populated automatically.

```go
client := relay.New(relay.WithDisableRedirectTracking())
```

---

## WithDisableRetry

```go
func WithDisableRetry() relay.Option
```

Disables all retry behaviour so only a single attempt is ever made (equivalent to `WithRetry(&relay.RetryConfig{MaxAttempts: 1})`).

**Default:** Not disabled; a client-wide default retry policy (3 attempts, exponential backoff) is active unless this option or `WithRetry` overrides it.

```go
client := relay.New(relay.WithDisableRetry())
```

---

## WithDisableTiming

```go
func WithDisableTiming() relay.Option
```

**Deprecated:** timing is off by default now (see `WithTiming`); this option is a no-op kept only for source compatibility with older code.

---

## WithDNSCache

```go
func WithDNSCache(ttl time.Duration) relay.Option
```

Enables client-side DNS caching so each unique hostname is resolved at most once per `ttl` interval, reducing lookup latency on keep-alive-heavy workloads. The cache is per-client. Entries are evicted lazily on next access after expiry. Concurrent misses for the same host are coalesced into a single real DNS query.

**Default:** Disabled; every dial uses the system resolver.

> **Tip:** A TTL of 30s-5min works well for most services, depending on how often upstream IPs rotate.

```go
client := relay.New(relay.WithDNSCache(30 * time.Second))
```

---

## WithDNSOverride

```go
func WithDNSOverride(hosts map[string]string) relay.Option
```

Maps hostnames to fixed IP addresses, bypassing DNS resolution for those hosts. Useful for service discovery, split-horizon DNS, and integration testing without modifying `/etc/hosts`. Calling this multiple times merges into the existing map.

**Default:** No overrides.

```go
client := relay.New(relay.WithDNSOverride(map[string]string{
    "api.internal": "10.0.0.42",
}))
```

---

## WithDynamicTLSCert

```go
func WithDynamicTLSCert(certFile, keyFile string, interval time.Duration) relay.Option
```

Enables hot-reloading of the TLS client certificate: the files are re-read every `interval`, so short-lived certificates (ACME, Vault PKI) get picked up without restarting the client or dropping existing connections. `interval` must be positive - construction fails silently (TLS config left unchanged) if the initial load fails, or the resulting `CertWatcher` is simply not created for `interval <= 0`. The watcher is exclusively owned by the client that created it and is stopped automatically by `Client.Shutdown`.

**Default:** Static TLS client certificate (or none); no hot-reload.

```go
client := relay.New(relay.WithDynamicTLSCert(
    "/etc/certs/client.crt", "/etc/certs/client.key", 5*time.Minute,
))
```

---

## WithErrorDecoder

```go
func WithErrorDecoder(fn func(statusCode int, body []byte) error) relay.Option
```

Called whenever the HTTP response status code is ≥ 400, after all `WithOnAfterResponse` hooks. Receives the status code and the full, already-buffered body. Returning a non-nil error releases the response and returns that error from `Client.Execute` instead; returning `nil` preserves the default behaviour where HTTP error codes are not automatically treated as Go errors.

**Default:** `nil`; 4xx/5xx responses are returned normally, not as errors.

```go
var ErrNotFound = errors.New("not found")

client := relay.New(relay.WithErrorDecoder(func(status int, body []byte) error {
    if status == http.StatusNotFound {
        return ErrNotFound
    }
    return nil
}))
```

---

## WithExpectContinueTimeout

```go
func WithExpectContinueTimeout(d time.Duration) relay.Option
```

Sets the maximum time to wait for a server's first response headers after fully writing the request headers, when the request has an `Expect: 100-continue` header. Zero disables the timeout.

**Default:** 0 (disabled).

```go
client := relay.New(relay.WithExpectContinueTimeout(1 * time.Second))
```

---

## WithExtension

```go
func WithExtension(ext relay.Extension) relay.Option

type Extension interface {
    Name() string
    Apply(cfg *relay.Config) error
}
```

Registers an `Extension` - a single seam bundling transport middleware, hooks, and construction-time config validation, instead of hand-wiring `WithTransportMiddleware` plus assorted hook options. Multiple extensions may be registered; `Apply` runs in registration order, once per extension, after all `With*` options have been applied. An `Apply` error is logged via the configured `Logger`, not fatal.

**Default:** No extensions registered.

```go
client := relay.New(relay.WithExtension(myExtension))
```

---

## WithHARRecording

```go
func WithHARRecording(rec *relay.HARRecorder) relay.Option
```

Attaches a `HARRecorder` that captures every request/response pair in HAR 1.2 format. Call `HARRecorder.Export`/`ExportHAR` to serialise. The recorded body snapshot is capped at 10 MB per request/response for memory safety; the actual request and response bodies sent/received over the wire are never truncated by this option.

**Default:** `nil`; no HAR recording.

```go
rec := relay.NewHARRecorder()
client := relay.New(relay.WithHARRecording(rec))
```

## WithHARRecorder

```go
func WithHARRecorder(rec *relay.HARRecorder) relay.Option
```

Alias for `WithHARRecording`.

---

## WithHealthCheck

```go
func WithHealthCheck(url string, interval, timeout time.Duration, expectedStatus int) relay.Option
```

Enables a background goroutine that proactively probes `url` with a plain GET while the circuit breaker is `Open`, resetting it to `Closed` on a successful probe instead of waiting for `ResetTimeout` to elapse naturally. `expectedStatus == 0` accepts any 2xx. Has no effect when the circuit breaker is disabled. The probe goroutine stops automatically on `Client.Shutdown`.

**Default:** Disabled. `interval <= 0` defaults to 1 minute; `timeout <= 0` defaults to 10 seconds.

```go
client := relay.New(relay.WithHealthCheck(
    "https://api.example.com/health", 30*time.Second, 5*time.Second, http.StatusOK,
))
```

---

## WithHedging

```go
func WithHedging(after time.Duration) relay.Option
```

Enables request hedging: if the first attempt hasn't completed within `after`, a duplicate request is sent; the first response to arrive wins and the other is cancelled. Defaults to 2 concurrent attempts total - use `WithHedgingN` to change that. Trades a small amount of extra backend load for reduced tail latency.

**Default:** Disabled (`0`, no hedging).

> **Warning:** Only hedge idempotent requests (GET, HEAD, PUT, DELETE, OPTIONS, TRACE). Hedging POST/PATCH can cause duplicate side effects unless the endpoint is itself idempotency-safe (see `WithAutoIdempotencyOnSafeRetries`).

```go
client := relay.New(relay.WithHedging(100 * time.Millisecond))
```

## WithHedgingN

```go
func WithHedgingN(after time.Duration, maxAttempts int) relay.Option
```

Like `WithHedging`, but with up to `maxAttempts` concurrent duplicate requests (including the original) instead of the default of 2. `after` is the delay between launching each successive attempt.

**Default:** Disabled.

```go
client := relay.New(relay.WithHedgingN(50*time.Millisecond, 3))
```

---

## WithHTTP2PushHandler

```go
func WithHTTP2PushHandler(handler relay.PushPromiseHandler) relay.Option

type PushPromiseHandler func(pushedURL string, pushedResp *http.Response)
```

Registers a handler for HTTP/2 server push promises.

> **Current limitation:** the version of `golang.org/x/net/http2` this module depends on disables server push at the transport level (`SETTINGS_ENABLE_PUSH=0`) and no longer exposes a push-interception API. This option is stored for forward compatibility but the handler is never actually invoked at runtime today.

**Default:** `nil`.

---

## WithIdleConnTimeout

```go
func WithIdleConnTimeout(d time.Duration) relay.Option
```

Sets how long an idle keep-alive connection remains open before being evicted from the pool.

**Default:** 90 seconds.

```go
client := relay.New(relay.WithIdleConnTimeout(60 * time.Second))
```

---

## WithInMemoryCache

```go
func WithInMemoryCache(maxEntries int) relay.Option
```

Creates and attaches the built-in in-memory LRU `CacheStore` with the given maximum entry count. Shorthand for `WithCache(relay.NewInMemoryCacheStore(maxEntries))`.

**Default:** No cache.

```go
client := relay.New(relay.WithInMemoryCache(1000))
```

---

## WithLoadBalancer

```go
func WithLoadBalancer(cfg relay.LoadBalancerConfig) relay.Option
```

Distributes requests across multiple backend URLs. When set, `WithBaseURL` is ignored and a backend is selected per request according to `Strategy`.

**relay.LoadBalancerConfig fields:**

| Field | Type | Description |
|-------|------|-------------|
| `Backends` | `[]string` | Base URLs to balance across. Must not be empty. |
| `Strategy` | `relay.LoadBalancerStrategy` | `relay.RoundRobin` (default) or `relay.Random`. |

**Default:** Disabled.

```go
client := relay.New(relay.WithLoadBalancer(relay.LoadBalancerConfig{
    Backends: []string{"https://api-1.example.com", "https://api-2.example.com"},
    Strategy: relay.RoundRobin,
}))
```

---

## WithLogger

```go
func WithLogger(l relay.Logger) relay.Option
```

Sets the structured logger used for internal relay events (retries, circuit-breaker transitions, rate-limit waits, extension errors). Use `relay.SlogAdapter(slog.Default())` to integrate with `log/slog`, or implement `relay.Logger` yourself.

**Default:** A no-op logger; nothing is logged.

```go
client := relay.New(relay.WithLogger(relay.SlogAdapter(slog.Default())))
```

---

## WithMaxConcurrentRequests

```go
func WithMaxConcurrentRequests(n int) relay.Option
```

Enables bulkhead isolation: at most `n` requests may be in flight simultaneously. Additional requests block until a slot frees up or the request's context is cancelled, in which case `relay.ErrBulkheadFull` (wrapping the context error) is returned. Pair with `WithPriorityQueue` to dequeue higher-priority requests first once a slot opens.

**Default:** `0`; no concurrency limit.

```go
client := relay.New(relay.WithMaxConcurrentRequests(50))
```

---

## WithMaxRedirects

```go
func WithMaxRedirects(n int) relay.Option
```

Sets the maximum number of redirects followed automatically. Set to `0` to disable redirect following entirely (the 3xx response is returned to the caller instead).

**Default:** 10.

```go
client := relay.New(relay.WithMaxRedirects(3))
```

---

## WithMaxResponseBodyBytes

```go
func WithMaxResponseBodyBytes(n int64) relay.Option
```

Limits how many bytes of a response body `Client.Execute` buffers. Responses exceeding the limit are truncated; check `Response.IsTruncated()`. Set to `0` for no limit. Can be overridden per request.

**Default:** 10 MB.

```go
client := relay.New(relay.WithMaxResponseBodyBytes(50 * 1024 * 1024)) // 50 MB
```

---

## WithOnAfterResponse

```go
func WithOnAfterResponse(hook func(context.Context, *relay.Response) error) relay.Option
```

Appends a hook called after a successful response is received (after all retries, before returning to the caller). A hook returning a non-nil error propagates as the `Client.Execute` return value. Runs before `WithErrorDecoder`.

**Default:** No hooks.

```go
client := relay.New(relay.WithOnAfterResponse(func(ctx context.Context, resp *relay.Response) error {
    log.Printf("response: %d", resp.StatusCode)
    return nil
}))
```

---

## WithOnBeforeRequest

```go
func WithOnBeforeRequest(hook func(context.Context, *relay.Request) error) relay.Option
```

Appends a hook called before each request attempt, including retries. A hook returning a non-nil error cancels the request immediately.

**Default:** No hooks.

```go
client := relay.New(relay.WithOnBeforeRequest(func(ctx context.Context, req *relay.Request) error {
    log.Printf("sending %s %s", req.Method(), req.URL())
    return nil
}))
```

---

## WithOnErrorHook

```go
func WithOnErrorHook(fn relay.OnErrorHookFunc) relay.Option

type OnErrorHookFunc func(ctx context.Context, req *relay.Request, err error)
```

Appends a hook invoked whenever `Client.Execute` returns a non-nil error, after all internal error handling (retries exhausted, etc.). Intended for logging/metrics; the return value is discarded.

**Default:** No hooks.

```go
client := relay.New(relay.WithOnErrorHook(
    func(ctx context.Context, req *relay.Request, err error) {
        log.Printf("request to %s failed: %v", req.URL(), err)
    },
))
```

---

## WithOnRetry

```go
func WithOnRetry(fn func(attempt int, resp *http.Response, err error)) relay.Option
```

Registers a callback invoked before each retry sleep, set directly on the active `RetryConfig` (creating a default one first if none is set yet). `attempt` is 1-based. Prefer `WithBeforeRetryHook` if you need the originating `*relay.Request` too.

**Default:** No callback.

```go
client := relay.New(relay.WithOnRetry(func(attempt int, resp *http.Response, err error) {
    log.Printf("retry attempt %d: %v", attempt, err)
}))
```

---

## WithOnStateChange

```go
func WithOnStateChange(fn func(from, to relay.CircuitBreakerState)) relay.Option
```

Registers a callback invoked on every circuit breaker state transition (`Closed`/`Open`/`HalfOpen`), set directly on the active `CircuitBreakerConfig` (creating a default one first if none is set yet).

**Default:** No callback.

```go
client := relay.New(relay.WithOnStateChange(func(from, to relay.CircuitBreakerState) {
    log.Printf("circuit breaker: %v -> %v", from, to)
}))
```

---

## WithPriorityQueue

```go
func WithPriorityQueue() relay.Option
```

Enables priority-aware dequeuing when the bulkhead (`WithMaxConcurrentRequests`) is at capacity: higher-priority requests are dequeued before lower-priority ones once a slot frees up, with FIFO order within the same priority. Only has an effect when `WithMaxConcurrentRequests` is also set.

**Default:** Disabled; waiting requests are served FIFO regardless of priority.

```go
client := relay.New(
    relay.WithMaxConcurrentRequests(20),
    relay.WithPriorityQueue(),
)
```

---

## WithProxy

```go
func WithProxy(proxyURL string) relay.Option
```

Sets the proxy URL for all requests. Pass `""` (or omit the option) to inherit the proxy from the `HTTP_PROXY`/`HTTPS_PROXY` environment variables instead.

**Default:** `""`; proxy sourced from environment variables.

```go
client := relay.New(relay.WithProxy("http://proxy.internal:8080"))
```

---

## WithRateLimit

```go
func WithRateLimit(rps float64, burst int) relay.Option
```

Enables a client-side token-bucket rate limiter. `rps` is the sustained requests-per-second rate; `burst` is the maximum tokens that can accumulate above the sustained rate (how many requests can fire immediately before throttling kicks in). Requests block in `Wait` until a token is available or the request's context is done.

**Default:** Disabled.

```go
client := relay.New(relay.WithRateLimit(100, 20))
```

---

## WithRequestCoalescing

```go
func WithRequestCoalescing() relay.Option
```

Enables deduplication of concurrent identical GET/HEAD requests: only one real HTTP call is made per unique key (method + URL + a fixed set of identity headers); all waiting callers receive independent copies of the response. The shared call runs under a detached context, so no individual caller's `Request.WithTimeout` bounds it - only the client-level `WithTimeout` does.

**Default:** Disabled.

```go
client := relay.New(relay.WithRequestCoalescing())
```

---

## WithRequestCompression

```go
func WithRequestCompression(algo relay.CompressionAlgorithm, minBytes int) relay.Option
```

Enables transparent compression of outgoing request bodies whose serialised size exceeds `minBytes` (pass `<= 0` to use the default of 1024 bytes). `Content-Encoding` is set automatically. `CompressionAuto`/`CompressionZstd` compress with zstd; `CompressionBrotli`/`CompressionGzip` use the named algorithm explicitly.

**Default:** Disabled; request bodies are never compressed.

```go
client := relay.New(relay.WithRequestCompression(relay.CompressionZstd, 512))
```

---

## WithRequestDeduplication

```go
func WithRequestDeduplication() relay.Option
```

Enables singleflight-based deduplication for GET/HEAD requests, collapsing concurrent requests to the same URL into a single real HTTP call. Similar to `WithRequestCoalescing` but implemented via `golang.org/x/sync/singleflight` on a simpler key (method + full URL only). Can be overridden per request with `Request.WithDeduplication`. The shared call runs under a detached context, same caveat as `WithRequestCoalescing`.

**Default:** Disabled.

```go
client := relay.New(relay.WithRequestDeduplication())
```

---

## WithRequestLogger

```go
func WithRequestLogger(logger relay.Logger) relay.Option
```

Adds transport-level middleware that logs every request/response cycle via `logger`. Requests are logged at Debug; responses at Debug for 2xx/3xx and Warn for 4xx/5xx, including status code and latency in milliseconds. Passing `nil` is a safe no-op.

**Default:** Not added.

```go
client := relay.New(relay.WithRequestLogger(relay.SlogAdapter(slog.Default())))
```

---

## WithResponseDecoder

```go
func WithResponseDecoder(fn func(contentType string, body []byte, v any) error) relay.Option
```

Replaces the default `encoding/json`/`encoding/xml` decoders used by `Response.Decode` and `relay.ExecuteAs`/`relay.DecodeAs`. `contentType` receives the response's `Content-Type` header so the function can pick the right format (e.g. Protocol Buffers).

**Default:** `nil`; built-in JSON/XML decoding based on Content-Type.

```go
client := relay.New(relay.WithResponseDecoder(func(ct string, body []byte, v any) error {
    if strings.Contains(ct, "protobuf") {
        return proto.Unmarshal(body, v.(proto.Message))
    }
    return json.Unmarshal(body, v)
}))
```

---

## WithResponseHeaderTimeout

```go
func WithResponseHeaderTimeout(d time.Duration) relay.Option
```

Sets the deadline to read response headers after the request body has been fully sent. `0` disables the timeout.

**Default:** `0` (disabled).

```go
client := relay.New(relay.WithResponseHeaderTimeout(10 * time.Second))
```

---

## WithResponseValidator

```go
func WithResponseValidator(v relay.SchemaValidator) relay.Option
```

Applies a `SchemaValidator` after each successful (2xx) response body is decoded. If validation fails, `Client.Execute` returns a `relay.ValidationError`. The built-in JSON Schema validator is one implementation of `SchemaValidator`.

**Default:** `nil`; no response validation.

```go
client := relay.New(relay.WithResponseValidator(mySchemaValidator))
```

---

## WithRetry

```go
func WithRetry(rc *relay.RetryConfig) relay.Option
```

Replaces the entire retry configuration. Pass `nil` to restore the package defaults.

**relay.RetryConfig fields:**

| Field | Type | Description | Package default |
|-------|------|-------------|------|
| `MaxAttempts` | `int` | Total tries including the first. `1` disables retries. | 3 |
| `InitialInterval` | `time.Duration` | Base delay before the first retry. | 100ms |
| `MaxInterval` | `time.Duration` | Caps the computed backoff regardless of attempt count. | 30s |
| `Multiplier` | `float64` | Growth factor per attempt (`2.0` = classic exponential backoff). | 2.0 |
| `RandomFactor` | `float64` | ± jitter proportional to the computed interval (`0` disables). | 0.5 |
| `RetryableStatus` | `[]int` | HTTP status codes that trigger a retry. | `[429, 500, 502, 503, 504]` |
| `RetryIf` | `func(resp *http.Response, err error) bool` | Optional predicate; returning `false` suppresses a retry the built-in logic would otherwise perform. | nil |
| `OnRetry` | `func(attempt int, resp *http.Response, err error)` | Optional callback before each retry sleep. | nil |

**Default:** The table's "Package default" column; retries are always active unless `WithDisableRetry` is used or `MaxAttempts: 1` is set explicitly.

```go
client := relay.New(relay.WithRetry(&relay.RetryConfig{
    MaxAttempts:     4,
    InitialInterval: 250 * time.Millisecond,
    MaxInterval:     8 * time.Second,
    Multiplier:      2.0,
    RandomFactor:    0.3,
}))
```

---

## WithRetryBudget

```go
func WithRetryBudget(b *relay.RetryBudget) relay.Option
```

Sets a sliding-window retry budget that caps the fraction of requests that may be retried within the window, preventing retry storms when a downstream service degrades.

**relay.RetryBudget fields:**

| Field | Type | Description |
|-------|------|-------------|
| `Ratio` | `float64` | Max fraction of requests that can be retried (e.g. `0.1` = 10%). |
| `Window` | `time.Duration` | Sliding window duration. |
| `MinRetry` | `int` | Minimum number of retries always allowed regardless of ratio (default 10 if unset). |

**Default:** `nil`; no budget enforced.

```go
client := relay.New(relay.WithRetryBudget(&relay.RetryBudget{
    Ratio:  0.1,
    Window: 10 * time.Second,
}))
```

---

## WithRetryIf

```go
func WithRetryIf(fn func(resp *http.Response, err error) bool) relay.Option
```

Sets a custom retry predicate on the active `RetryConfig` (creating a default one first if none is set yet). Evaluated when the built-in logic would retry; returning `false` suppresses that retry.

**Default:** No custom predicate; the built-in status/error classification decides.

```go
client := relay.New(relay.WithRetryIf(func(resp *http.Response, err error) bool {
    if resp != nil {
        return resp.StatusCode == http.StatusServiceUnavailable
    }
    return true
}))
```

---

## WithRootCA

```go
func WithRootCA(caPEM []byte) relay.Option
```

Adds a PEM-encoded CA certificate to the client's TLS trust store, for private PKI environments where the server certificate is signed by an internal CA. Multiple calls append additional CAs without replacing earlier ones.

**Default:** The system certificate pool only.

```go
client := relay.New(relay.WithRootCA(internalCAPEM))
```

---

## WithSigner

```go
func WithSigner(s relay.RequestSigner) relay.Option

type RequestSigner interface {
    Sign(req *http.Request) error
}
```

Sets a `RequestSigner` invoked for every outgoing request (including retries), after default headers, the idempotency key, and `WithCredentialProvider` have been applied. The signer receives the fully-built `*http.Request` and may add/modify headers or compute a body digest. Use `relay.RequestSignerFunc` to adapt a plain function. Built-in implementation: `ext/sigv4` (AWS SigV4). For your own HMAC/JWS/OAuth 1.0a scheme, implement `RequestSigner` directly.

**Default:** `nil`; no signing.

```go
client := relay.New(relay.WithSigner(relay.RequestSignerFunc(func(r *http.Request) error {
    r.Header.Set("Authorization", "Bearer "+apiKey)
    return nil
})))
```

## WithRequestSigner

```go
func WithRequestSigner(s relay.RequestSigner) relay.Option
```

Alias for `WithSigner`.

---

## WithSRVDiscovery

```go
func WithSRVDiscovery(resolver *relay.SRVResolver) relay.Option
```

Sets an `SRVResolver` on the client. Before each request, the resolver performs (or serves from cache) a DNS SRV lookup and the request's `Host` is replaced with the resolved `host:port` target. Build the resolver with `relay.NewSRVResolver(service, proto, name, scheme, opts...)`, where `opts` are `relay.SRVOption` values - `WithSRVTTL(d)` to cache lookups, `WithSRVBalancer(b)` to pick `relay.SRVRoundRobin` (default), `relay.SRVRandom`, or `relay.SRVPriority`. Note: `WithSRVTTL`/`WithSRVBalancer` configure the resolver itself (`SRVOption`), not the client (`relay.Option`) - they're passed to `NewSRVResolver`, not to `relay.New`.

**Default:** No SRV-based discovery.

```go
resolver := relay.NewSRVResolver("http", "tcp", "myservice.example.com", "https",
    relay.WithSRVTTL(30*time.Second),
    relay.WithSRVBalancer(relay.SRVRoundRobin),
)
client := relay.New(relay.WithSRVDiscovery(resolver))
```

---

## WithTimeout

```go
func WithTimeout(d time.Duration) relay.Option
```

Sets the end-to-end deadline for a complete request/response cycle, including all retry attempts. Use `Request.WithTimeout` for per-request overrides.

**Default:** 30 seconds.

```go
client := relay.New(relay.WithTimeout(10 * time.Second))
```

---

## WithTiming

```go
func WithTiming() relay.Option
```

Enables per-request timing instrumentation (`httptrace`). When enabled, `Response.Timing` is populated with a DNS/TCP/TLS/TTFB/content-transfer breakdown at the cost of roughly 10 additional allocations per `Client.Execute` call. Automatically enabled by `WithAdaptiveTimeout`, since adaptive timeout needs latency samples to function.

**Default:** Off; `Response.Timing` fields stay zero unless timing is enabled directly or via `WithAdaptiveTimeout`.

```go
client := relay.New(relay.WithTiming())
```

---

## WithTLSConfig

```go
func WithTLSConfig(tlsCfg *tls.Config) relay.Option
```

Replaces the default `*tls.Config` used by the transport. Use this for mTLS beyond what `WithClientCert`/`WithClientCertPEM` cover, custom root CAs beyond `WithRootCA`, or minimum TLS version enforcement.

**Default:** A config enforcing TLS 1.2 as the minimum version.

```go
client := relay.New(relay.WithTLSConfig(&tls.Config{
    MinVersion: tls.VersionTLS13,
}))
```

---

## WithTLSHandshakeTimeout

```go
func WithTLSHandshakeTimeout(d time.Duration) relay.Option
```

Sets the deadline for completing the TLS handshake.

**Default:** 10 seconds.

```go
client := relay.New(relay.WithTLSHandshakeTimeout(5 * time.Second))
```

---

## WithTransportAdapter

```go
func WithTransportAdapter(scheme string, rt http.RoundTripper) relay.Option
```

Registers a custom `http.RoundTripper` for requests whose URL scheme matches `scheme` (e.g. `"h3"`, `"grpc"`). Requests with that scheme are dispatched to `rt` instead of the default transport. `"http"`/`"https"` cannot be overridden this way - use `WithTransportMiddleware` for those. Used internally by `ext/http3` to route `h3://` requests over QUIC.

**Default:** Only `http`/`https` are handled, by the default transport.

```go
client := relay.New(relay.WithTransportAdapter("h3", http3Transport))
```

---

## WithTransportMiddleware

```go
func WithTransportMiddleware(mw ...func(http.RoundTripper) http.RoundTripper) relay.Option
```

Appends one or more `http.RoundTripper` middleware functions. Middleware is applied outermost-last: the *last* appended middleware is the *first* to intercept a request. This is the general-purpose way to add cross-cutting transport concerns (logging, tracing, header injection).

**Default:** No middleware.

```go
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

client := relay.New(relay.WithTransportMiddleware(
    func(next http.RoundTripper) http.RoundTripper {
        return roundTripFunc(func(req *http.Request) (*http.Response, error) {
            req.Header.Set("X-Correlation-ID", generateCorrelationID())
            return next.RoundTrip(req)
        })
    },
))
```

---

## WithURLNormalisation

```go
func WithURLNormalisation(mode relay.URLNormalisationMode) relay.Option
```

Controls how `WithBaseURL` is resolved against request paths. `relay.NormalisationAuto` (default) intelligently detects API-shaped base URLs (e.g. `/v1`, `/odata`, `/graphql`) and preserves their path via safe string concatenation, while using zero-allocation RFC 3986 resolution for host-only base URLs. `relay.NormalisationRFC3986` forces strict RFC 3986 resolution everywhere (fastest, but a base URL with a path component like `/v1` gets replaced rather than extended by an absolute request path). `relay.NormalisationAPI` forces safe string normalisation everywhere.

**Default:** `relay.NormalisationAuto`.

```go
client := relay.New(relay.WithURLNormalisation(relay.NormalisationAPI))
```

---

## WithUnixSocket

```go
func WithUnixSocket(socketPath string) relay.Option
```

Routes all requests through a Unix domain socket at `socketPath`, regardless of the host in the request URL (e.g. talking to the Docker daemon at `/var/run/docker.sock`). `WithBaseURL` still controls the HTTP `Host` header and path; only the network transport layer changes. HTTP/2 is disabled for Unix socket connections. No-op on `js`/`wasm` builds.

**Default:** TCP.

```go
client := relay.New(
    relay.WithBaseURL("http://localhost"),
    relay.WithUnixSocket("/var/run/docker.sock"),
)
```

---

## WithWebSocketDialTimeout

```go
func WithWebSocketDialTimeout(d time.Duration) relay.Option
```

Sets the handshake timeout used by `Client.ExecuteWebSocket`. Zero falls back to the client's `WithTimeout` value.

**Default:** `0` (falls back to `WithTimeout`).

```go
client := relay.New(relay.WithWebSocketDialTimeout(10 * time.Second))
```

---

## Per-request authentication (not client options)

A static API key, Basic Auth, or Bearer token attached to *every* request from a single client is most often set once via `WithDefaultHeaders`, `WithSigner`, or `WithCredentialProvider` above. But relay also has dedicated per-request builder methods on `*relay.Request` for the common cases, set individually on whichever requests need them:

```go
req := client.Get("/orders").
    WithBearerToken(token).       // Authorization: Bearer <token>
    WithBasicAuth(user, pass).    // Authorization: Basic <base64>
    WithAPIKey("X-API-Key", key)  // arbitrary header-based key
```

These are methods on `*relay.Request`, not `relay.Option` functions - they cannot be passed to `relay.New`.

---

## Complete Configuration Example

The following example shows a production-grade client combining multiple options:

```go
package main

import (
    "context"
    "crypto/tls"
    "log"
    "net/http"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithDefaultHeaders(map[string]string{
            "Authorization": "Bearer prod-token-value",
        }),
        relay.WithTimeout(15*time.Second),
        relay.WithRetry(&relay.RetryConfig{
            MaxAttempts:     4,
            InitialInterval: 250 * time.Millisecond,
            MaxInterval:     8 * time.Second,
            Multiplier:      2.0,
            RandomFactor:    0.5,
        }),
        relay.WithAutoIdempotencyOnSafeRetries(),
        relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
            MaxFailures:      10,
            ResetTimeout:     1 * time.Minute,
            SuccessThreshold: 3,
        }),
        relay.WithRateLimit(500, 50),
        relay.WithMaxConcurrentRequests(100),
        relay.WithDNSCache(30*time.Second),
        relay.WithTLSConfig(&tls.Config{
            MinVersion: tls.VersionTLS12,
        }),
        relay.WithBeforeRetryHook(func(ctx context.Context, attempt int, req *relay.Request, resp *http.Response, err error) {
            log.Printf("[retry] attempt %d for %s: %v", attempt, req.URL(), err)
        }),
        relay.WithOnErrorHook(func(ctx context.Context, req *relay.Request, err error) {
            log.Printf("[error] %s -> %v", req.URL(), err)
        }),
    )
    defer client.Shutdown(context.Background())

    resp, err := client.Execute(client.Get("/health"))
    if err != nil {
        log.Fatal(err)
    }
    defer relay.PutResponse(resp)
    log.Println("health check:", resp.StatusCode)
}
```
