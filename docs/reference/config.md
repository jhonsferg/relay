# Configuration Reference

All relay client configuration is applied through functional option functions passed to `relay.New`. Each function returns a `relay.Option` value. Options are applied in order, so later options override earlier ones where applicable.

This reference lists all `With*` option functions in alphabetical order.

---

## WithAPIKey

```go
func WithAPIKey(header, key string) relay.Option
```

Attaches a static API key to every request by setting the specified HTTP header. This is the correct option for APIs that authenticate via a custom header (e.g., `X-API-Key`, `Api-Token`) rather than the standard `Authorization` header.

**Default:** No API key header is added.

**Parameters:**
- `header` - The HTTP header name (e.g., `"X-API-Key"`).
- `key` - The key value to set on that header.

```go
client := relay.New(relay.WithAPIKey("X-API-Key", "secret-key-value"))
```

---

## WithAutoIdempotencyOnSafeRetries

```go
func WithAutoIdempotencyOnSafeRetries() relay.Option
```

Automatically injects an `Idempotency-Key` UUID header on retried requests for methods that are semantically safe to retry with idempotency guarantees (POST, PATCH). GET, HEAD, PUT, and DELETE are idempotent by definition and are not modified. The key is generated fresh for each original request and reused on its retries.

**Default:** Disabled.

> **Note:** This option requires `WithRetry` to be configured. Without retries, no idempotency key is ever injected.

```go
client := relay.New(
    relay.WithAutoIdempotencyOnSafeRetries(),
    relay.WithRetry(&relay.RetryConfig{MaxAttempts: 3}),
)
```

---

## WithBaseURL

```go
func WithBaseURL(baseURL string) relay.Option
```

Sets the base URL that is prepended to all relative paths used in `client.Get`, `client.Post`, and the other method helpers. The base URL should not have a trailing slash; relay handles path joining correctly.

**Default:** Empty string (requests must use absolute URLs or per-request overrides).

```go
client := relay.New(relay.WithBaseURL("https://api.example.com/v2"))
```

---

## WithBasicAuth

```go
func WithBasicAuth(username, password string) relay.Option
```

Configures HTTP Basic Authentication on every request. The username and password are Base64-encoded and set on the `Authorization` header as per RFC 7617.

**Default:** No Basic Auth header is attached.

> **Warning:** Basic Auth credentials are transmitted with every request. Ensure TLS is used to prevent credential exposure.

```go
client := relay.New(relay.WithBasicAuth("admin", "hunter2"))
```

---

## WithBearerToken

```go
func WithBearerToken(token string) relay.Option
```

Sets a static Bearer token on the `Authorization` header for every request in the format `Authorization: Bearer <token>`. Use this for long-lived tokens. For short-lived OAuth2 tokens that need automatic refresh, use `WithOAuth2` instead.

**Default:** No Authorization header is added.

```go
client := relay.New(relay.WithBearerToken("eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9..."))
```

---

## WithBeforeRedirectHook

```go
func WithBeforeRedirectHook(fn func(*relay.Request, []*http.Request, *http.Response) error) relay.Option
```

Registers a callback invoked before each redirect is followed. The callback receives the current relay request, the chain of HTTP requests followed so far, and the redirect response. Return a non-nil error to abort the redirect chain.

**Default:** No hook; all redirects within `MaxRedirects` are followed automatically.

```go
client := relay.New(relay.WithBeforeRedirectHook(
    func(req *relay.Request, chain []*http.Request, resp *http.Response) error {
        log.Printf("redirecting to %s", resp.Header.Get("Location"))
        return nil
    },
))
```

---

## WithBeforeRetryHook

```go
func WithBeforeRetryHook(fn func(*relay.Request, int, error)) relay.Option
```

Registers a callback invoked before each retry attempt. The function receives the request being retried, the attempt number (1-indexed), and the error that triggered the retry. Use this for logging, metrics, or dynamic header mutation between retries.

**Default:** No hook.

```go
client := relay.New(relay.WithBeforeRetryHook(
    func(req *relay.Request, attempt int, err error) {
        log.Printf("retry #%d for %s: %v", attempt, req.URL(), err)
    },
))
```

---

## WithCircuitBreaker

```go
func WithCircuitBreaker(config *relay.CircuitBreakerConfig) relay.Option
```

Enables a circuit breaker that stops sending requests when a configurable failure threshold is reached, protecting downstream services from cascading failures.

**relay.CircuitBreakerConfig fields:**

| Field | Type | Description |
|-------|------|-------------|
| `MaxFailures` | `int` | Number of consecutive failures before tripping the circuit. |
| `Timeout` | `time.Duration` | How long to keep the circuit open before attempting a probe request. |
| `SuccessThreshold` | `int` | Number of successes in the half-open state required to close the circuit. |

**Default:** Circuit breaker is disabled.

```go
client := relay.New(relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
    MaxFailures:      5,
    Timeout:          30 * time.Second,
    SuccessThreshold: 2,
}))
```

---

## WithContentTypeDecoder

```go
func WithContentTypeDecoder(contentType string, fn func([]byte, interface{}) error) relay.Option
```

Registers a custom decoder for a specific MIME type. When `relay.DecodeAs` encounters a response with this Content-Type, it will use the provided function to unmarshal the body. This allows supporting non-standard formats like MessagePack, CBOR, or custom binary protocols.

**Default:** JSON (`application/json`) and XML (`application/xml`, `text/xml`) decoders are built in.

```go
import "github.com/vmihailenco/msgpack/v5"

client := relay.New(relay.WithContentTypeDecoder(
    "application/msgpack",
    func(data []byte, v interface{}) error {
        return msgpack.Unmarshal(data, v)
    },
))
```

---

## WithContentTypeEncoder

```go
func WithContentTypeEncoder(contentType string, fn func(interface{}) ([]byte, error)) relay.Option
```

Registers a custom encoder for a specific MIME type. When a request body is set and the `Content-Type` header matches, relay will encode using this function instead of the default JSON encoder.

**Default:** JSON encoder for `application/json` is built in.

```go
import "github.com/vmihailenco/msgpack/v5"

client := relay.New(relay.WithContentTypeEncoder(
    "application/msgpack",
    func(v interface{}) ([]byte, error) {
        return msgpack.Marshal(v)
    },
))
```

---

## WithDefaultAccept

```go
func WithDefaultAccept(mediaType string) relay.Option
```

Sets the `Accept` header on every request that does not already specify one. Useful when an API expects a specific media type for all responses (e.g., GitHub's `application/vnd.github+json`).

**Default:** No default `Accept` header is added; Go's HTTP client sends `*/*` implicitly.

```go
client := relay.New(relay.WithDefaultAccept("application/vnd.api+json"))
```

---

## WithDigestAuth

```go
func WithDigestAuth(username, password string) relay.Option
```

Enables HTTP Digest Authentication (RFC 7616). relay performs the initial unauthenticated request, receives the 401 challenge, and automatically responds with the computed digest credentials. This is more secure than Basic Auth because the password is never transmitted in plaintext.

**Default:** Digest Auth is disabled.

```go
client := relay.New(relay.WithDigestAuth("admin", "password123"))
```

---

## WithDisableRedirect

```go
func WithDisableRedirect() relay.Option
```

Disables automatic redirect following entirely. When the server returns a 3xx response, relay returns it to the caller instead of following the redirect. Use this when you need to inspect or handle redirects manually.

**Default:** Redirects are followed automatically up to `MaxRedirects`.

```go
client := relay.New(relay.WithDisableRedirect())
```

---

## WithDNSCache

```go
func WithDNSCache(ttl time.Duration) relay.Option
```

Enables an in-process DNS cache with the specified TTL. Cached entries are refreshed in the background when they expire. This can significantly reduce DNS lookup latency for high-frequency clients hitting the same hostnames.

**Default:** DNS caching is disabled; every connection uses the system resolver.

> **Tip:** A TTL of 30 seconds works well for most service meshes. Avoid very long TTLs in environments with frequent pod restarts or DNS-based failover.

```go
client := relay.New(relay.WithDNSCache(30 * time.Second))
```

---

## WithDynamicTLSCert

```go
func WithDynamicTLSCert(certFile, keyFile string) relay.Option
```

Configures a `CertWatcher` that monitors the given certificate and key files for changes and automatically reloads them without restarting the client or dropping existing connections. This is essential for services using short-lived certificates from ACME providers or Vault PKI.

**Default:** Static TLS configuration; certificates are not reloaded at runtime.

```go
client := relay.New(relay.WithDynamicTLSCert(
    "/etc/certs/client.crt",
    "/etc/certs/client.key",
))
```

---

## WithHedging

```go
func WithHedging(after time.Duration) relay.Option
```

Enables hedged requests. If the primary request has not completed within `after`, relay sends a second identical request in parallel and returns whichever response arrives first, cancelling the other. This trades a small increase in backend load for reduced tail latency (p99/p999).

**Default:** Hedging is disabled.

> **Warning:** Only use hedging for idempotent requests (GET, HEAD, PUT, DELETE). Hedging POST or PATCH requests can cause duplicate side effects unless `WithAutoIdempotencyOnSafeRetries` is also enabled.

```go
client := relay.New(relay.WithHedging(100 * time.Millisecond))
```

---

## WithHMACSign

```go
func WithHMACSign(secret, algorithm string) relay.Option
```

Adds HMAC request signing. Before each request is sent, relay computes an HMAC signature over a canonical representation of the request (method, path, body hash, timestamp) using the specified algorithm (`sha256`, `sha512`) and appends the signature to the `Authorization` header in a format compatible with common API signature schemes.

**Default:** HMAC signing is disabled.

```go
client := relay.New(relay.WithHMACSign("my-signing-secret", "sha256"))
```

---

## WithMaxConcurrentRequests

```go
func WithMaxConcurrentRequests(n int) relay.Option
```

Enables bulkhead isolation by limiting the number of requests that can be in-flight simultaneously. Additional requests block until a slot is available or the request context is cancelled. Returns `relay.ErrBulkheadFull` if the context is cancelled while waiting.

**Default:** No concurrency limit.

```go
client := relay.New(relay.WithMaxConcurrentRequests(50))
```

---

## WithMaxRedirects

```go
func WithMaxRedirects(n int) relay.Option
```

Sets the maximum number of redirects relay will follow for a single request. If the redirect count exceeds this value, an error is returned.

**Default:** 10 redirects.

```go
client := relay.New(relay.WithMaxRedirects(3))
```

---

## WithOAuth2

```go
func WithOAuth2(config *relay.OAuth2Config) relay.Option
```

Configures OAuth2 token management. relay automatically fetches tokens using the configured credentials, caches them, and refreshes them before expiry. Supports Client Credentials, Authorization Code (with PKCE), and Resource Owner Password grants.

**relay.OAuth2Config fields:**

| Field | Type | Description |
|-------|------|-------------|
| `TokenURL` | `string` | The token endpoint URL. |
| `ClientID` | `string` | OAuth2 client identifier. |
| `ClientSecret` | `string` | OAuth2 client secret. |
| `Scopes` | `[]string` | Requested OAuth2 scopes. |
| `GrantType` | `string` | One of `"client_credentials"`, `"password"`, `"authorization_code"`. |

**Default:** OAuth2 is disabled.

```go
client := relay.New(relay.WithOAuth2(&relay.OAuth2Config{
    TokenURL:     "https://auth.example.com/oauth/token",
    ClientID:     "my-service",
    ClientSecret: "s3cr3t",
    Scopes:       []string{"read:orders", "write:orders"},
    GrantType:    "client_credentials",
}))
```

---

## WithOnErrorHook

```go
func WithOnErrorHook(fn func(*relay.Request, error)) relay.Option
```

Registers a callback invoked whenever `Execute` returns a non-nil error. This fires after all retries are exhausted. Use this to centralize error logging, emit metrics, or trigger alerting without wrapping every call site.

**Default:** No hook.

```go
client := relay.New(relay.WithOnErrorHook(
    func(req *relay.Request, err error) {
        log.Printf("request to %s failed: %v", req.URL(), err)
    },
))
```

---

## WithRateLimit

```go
func WithRateLimit(config *relay.RateLimitConfig) relay.Option
```

Enables client-side rate limiting using a token bucket algorithm. Requests block until a token is available or the request context is cancelled.

**relay.RateLimitConfig fields:**

| Field | Type | Description |
|-------|------|-------------|
| `RequestsPerSecond` | `float64` | Sustained request rate (token bucket refill rate). |
| `Burst` | `int` | Maximum burst size above the sustained rate. |

**Default:** No rate limiting.

```go
client := relay.New(relay.WithRateLimit(&relay.RateLimitConfig{
    RequestsPerSecond: 100,
    Burst:             20,
}))
```

---

## WithRetry

```go
func WithRetry(config *relay.RetryConfig) relay.Option
```

Configures automatic retry behavior with exponential backoff and jitter. Retries are only performed for errors identified as transient by `relay.IsRetryableError` (network errors, 429, 5xx) unless custom retry conditions are specified.

**relay.RetryConfig fields:**

| Field | Type | Description |
|-------|------|-------------|
| `MaxAttempts` | `int` | Total attempts including the first (e.g., 3 means 1 try plus 2 retries). |
| `WaitMin` | `time.Duration` | Minimum backoff between attempts. |
| `WaitMax` | `time.Duration` | Maximum backoff (caps exponential growth). |
| `RetryOn` | `func(error) bool` | Custom predicate for retryable errors. If nil, uses `relay.IsRetryableError`. |

**Default:** No retries; requests are attempted exactly once.

```go
client := relay.New(relay.WithRetry(&relay.RetryConfig{
    MaxAttempts: 4,
    WaitMin:     100 * time.Millisecond,
    WaitMax:     5 * time.Second,
}))
```

---

## WithTimeout

```go
func WithTimeout(d time.Duration) relay.Option
```

Sets the default request timeout applied to every request executed by this client. This timeout covers the entire request lifecycle: connection, TLS handshake, sending the request, and reading the response body. It can be overridden per request with `req.WithTimeout`.

**Default:** 30 seconds.

```go
client := relay.New(relay.WithTimeout(10 * time.Second))
```

---

## WithTLSConfig

```go
func WithTLSConfig(config *tls.Config) relay.Option
```

Provides a custom `*tls.Config` for the underlying HTTP transport. Use this to configure mutual TLS (mTLS), custom root CAs, certificate pinning, or minimum TLS version enforcement.

**Default:** Go's default TLS configuration.

```go
import "crypto/tls"

client := relay.New(relay.WithTLSConfig(&tls.Config{
    MinVersion: tls.VersionTLS13,
    ServerName: "api.example.com",
}))
```

---

## WithTransport

```go
func WithTransport(t http.RoundTripper) relay.Option
```

Replaces the entire HTTP transport with the provided `http.RoundTripper`. This is a low-level escape hatch for scenarios requiring complete control over transport behavior, such as using a custom proxy, implementing request interception in tests, or integrating with specialized HTTP clients.

**Default:** A tuned `*http.Transport` with connection pooling enabled.

> **Warning:** Replacing the transport disables relay's built-in connection pool tuning. Prefer `WithTransportMiddleware` for wrapping rather than replacing the transport.

```go
client := relay.New(relay.WithTransport(myCustomTransport))
```

---

## WithTransportAdapter

```go
func WithTransportAdapter(scheme string, t http.RoundTripper) relay.Option
```

Registers a custom transport for a specific URL scheme. This enables relay to dispatch requests with non-standard schemes to specialized transports while routing standard `http://` and `https://` requests through the default transport. Used by the `ext/http3` extension to route `h3://` requests over QUIC.

**Default:** Only `http` and `https` schemes are handled by the default transport.

```go
client := relay.New(relay.WithTransportAdapter("grpc", grpcTransport))
```

---

## WithTransportMiddleware

```go
func WithTransportMiddleware(fn func(http.RoundTripper) http.RoundTripper) relay.Option
```

Wraps the current transport with a middleware function. Multiple calls to `WithTransportMiddleware` stack the middleware in order (first registered is outermost). This is the preferred way to add cross-cutting transport concerns like logging, tracing, or custom header injection at the transport layer.

**Default:** No transport middleware.

```go
client := relay.New(relay.WithTransportMiddleware(
    func(next http.RoundTripper) http.RoundTripper {
        return relay.RoundTripFunc(func(req *http.Request) (*http.Response, error) {
            req.Header.Set("X-Correlation-ID", generateCorrelationID())
            return next.RoundTrip(req)
        })
    },
))
```

---

## Complete Configuration Example

The following example shows a production-grade client combining multiple options:

```go
package main

import (
    "context"
    "crypto/tls"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithBearerToken("prod-token-value"),
        relay.WithTimeout(15*time.Second),
        relay.WithRetry(&relay.RetryConfig{
            MaxAttempts: 4,
            WaitMin:     250 * time.Millisecond,
            WaitMax:     8 * time.Second,
        }),
        relay.WithAutoIdempotencyOnSafeRetries(),
        relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
            MaxFailures:      10,
            Timeout:          1 * time.Minute,
            SuccessThreshold: 3,
        }),
        relay.WithRateLimit(&relay.RateLimitConfig{
            RequestsPerSecond: 500,
            Burst:             50,
        }),
        relay.WithMaxConcurrentRequests(100),
        relay.WithDNSCache(30*time.Second),
        relay.WithTLSConfig(&tls.Config{
            MinVersion: tls.VersionTLS12,
        }),
        relay.WithBeforeRetryHook(func(req *relay.Request, attempt int, err error) {
            log.Printf("[retry] attempt %d for %s: %v", attempt, req.URL(), err)
        }),
        relay.WithOnErrorHook(func(req *relay.Request, err error) {
            log.Printf("[error] %s -> %v", req.URL(), err)
        }),
    )

    resp, err := client.Execute(context.Background(), client.Get("/health"))
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    log.Println("health check:", resp.StatusCode)
}
```
