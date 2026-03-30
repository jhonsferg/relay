package relay

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/cookiejar"
	"time"
)

// Default values applied when no explicit option is provided.
// All of these can be overridden with the corresponding With* option.
const (
	defaultTimeout              = 30 * time.Second
	defaultMaxIdleConns         = 100
	defaultMaxIdleConnsPerHost  = 20
	defaultMaxConnsPerHost      = 50
	defaultIdleConnTimeout      = 90 * time.Second
	defaultTLSHandshakeTimeout  = 10 * time.Second
	defaultMaxRedirects         = 10
	defaultDialTimeout          = 30 * time.Second
	defaultDialKeepAlive        = 30 * time.Second
	defaultMaxResponseBodyBytes = 10 * 1024 * 1024 // 10 MB
)

// Config holds all configuration for a [Client]. It is built by applying a
// sequence of functional [Option] values on top of the defaults returned by
// [defaultConfig]. Do not modify a Config after passing it to [buildClient].
type Config struct {
	// BaseURL is prepended to every request path that does not start with
	// "http://" or "https://". Trailing slashes are normalised automatically.
	BaseURL string

	// Timeout is the end-to-end deadline for a complete request/response cycle,
	// including all retry attempts. Set per-request timeouts via
	// [Request.WithTimeout] for finer-grained control.
	Timeout time.Duration

	// MaxIdleConns is the maximum total number of idle (keep-alive) connections
	// across all hosts in the pool.
	MaxIdleConns int

	// MaxIdleConnsPerHost is the maximum number of idle connections kept per
	// individual host.
	MaxIdleConnsPerHost int

	// MaxConnsPerHost limits the total number of connections (active + idle) per
	// host. 0 means unlimited.
	MaxConnsPerHost int

	// IdleConnTimeout is how long an idle keep-alive connection remains open
	// before being evicted from the pool.
	IdleConnTimeout time.Duration

	// TLSHandshakeTimeout is the deadline for completing the TLS handshake.
	TLSHandshakeTimeout time.Duration

	// ResponseHeaderTimeout is the deadline to read the response headers after
	// the request body has been sent. 0 disables the timeout.
	ResponseHeaderTimeout time.Duration

	// DialTimeout is the maximum time allowed for a TCP connection to be
	// established.
	DialTimeout time.Duration

	// DialKeepAlive is the interval between TCP keep-alive probes sent on an
	// active connection.
	DialKeepAlive time.Duration

	// TLSConfig replaces the default TLS configuration. When nil, a config
	// enforcing TLS 1.2 as the minimum version is used.
	TLSConfig *tls.Config

	// ProxyURL is the URL of the HTTP/HTTPS proxy to use for all requests.
	// When empty, the proxy is sourced from the HTTP_PROXY / HTTPS_PROXY
	// environment variables.
	ProxyURL string

	// CookieJar manages cookie storage across requests. Set to nil to disable
	// automatic cookie handling.
	CookieJar http.CookieJar

	// CacheStore is the backend used to cache HTTP responses. When nil, caching
	// is disabled. Use [NewInMemoryCacheStore] or implement [CacheStore] for
	// custom backends such as Redis.
	CacheStore CacheStore

	// CustomDialer replaces the default net.Dialer. When set, DialTimeout and
	// DialKeepAlive are ignored in favour of the dialer's own settings.
	CustomDialer *net.Dialer

	// DNSOverrides maps hostnames to fixed IP addresses, bypassing DNS
	// resolution for those hosts. Example: {"api.internal": "10.0.0.42"}.
	DNSOverrides map[string]string

	// RetryConfig controls retry behavior. When nil, a sensible default
	// (3 attempts, exponential backoff) is used.
	RetryConfig *RetryConfig

	// CircuitBreakerConfig controls the circuit breaker. When nil, a default
	// (5 failures → Open, 60 s reset) is used. Set explicitly to nil with
	// [WithDisableCircuitBreaker] to disable it entirely.
	CircuitBreakerConfig *CircuitBreakerConfig

	// RateLimitConfig enables the client-side token-bucket rate limiter.
	// When nil, rate limiting is disabled.
	RateLimitConfig *RateLimitConfig

	// DefaultHeaders are merged into every outgoing request. Per-request headers
	// always take precedence over these defaults.
	DefaultHeaders map[string]string

	// DisableCompression disables automatic Accept-Encoding negotiation and
	// transparent response decompression.
	DisableCompression bool

	// MaxRedirects is the maximum number of redirects to follow automatically.
	// Set to 0 to disable redirect following.
	MaxRedirects int

	// MaxResponseBodyBytes is the maximum number of body bytes buffered by
	// [Execute]. Responses exceeding this limit are truncated and
	// [Response.IsTruncated] returns true. Set to 0 for no limit (default 10 MB).
	MaxResponseBodyBytes int64

	// TransportMiddlewares is a chain of [http.RoundTripper] wrappers applied
	// around the core transport. Middleware is applied outermost-last — the last
	// appended middleware is the first to intercept a request.
	TransportMiddlewares []func(http.RoundTripper) http.RoundTripper

	// OnBeforeRequest is a list of hooks called before each request attempt
	// (including retries). A hook that returns a non-nil error cancels the
	// request immediately.
	OnBeforeRequest []func(context.Context, *Request) error

	// OnAfterResponse is a list of hooks called after a successful response is
	// received (after all retries, before returning to the caller). A hook that
	// returns a non-nil error propagates as the [Client.Execute] return value.
	OnAfterResponse []func(context.Context, *Response) error

	// Logger is used for internal structured logging (retries, circuit-breaker
	// transitions, rate-limit events, shutdown). Defaults to NoopLogger.
	Logger Logger

	// TLSPins is a list of SHA-256 certificate fingerprints in the format
	// "sha256/BASE64==". When non-empty, TLS connections whose certificate chain
	// does not match any pin are rejected with ErrCertificatePinMismatch.
	TLSPins []string

	// EnableCoalescing activates request deduplication for concurrent identical
	// GET/HEAD requests. Only one real request is made; all waiters share a copy.
	EnableCoalescing bool

	// DigestUsername and DigestPassword enable HTTP Digest Authentication.
	DigestUsername string
	DigestPassword string

	// HARRecorder captures all request/response pairs when non-nil.
	HARRecorder *HARRecorder

	// AutoIdempotencyKey automatically injects an X-Idempotency-Key header on
	// every request. The same key is reused across retry attempts.
	AutoIdempotencyKey bool

	// HealthCheck enables a background goroutine that proactively probes a
	// health endpoint while the circuit breaker is open and resets it on
	// success. Nil disables the feature.
	HealthCheck *HealthCheckConfig

	// DNSCache enables client-side DNS caching with a configurable TTL.
	// Nil disables the feature (default: OS resolver on every dial).
	DNSCache *DNSCacheConfig
}

// defaultConfig returns a Config populated with all production-ready defaults.
// Every With* option is applied on top of this baseline.
func defaultConfig() *Config {
	return &Config{
		Timeout:              defaultTimeout,
		MaxIdleConns:         defaultMaxIdleConns,
		MaxIdleConnsPerHost:  defaultMaxIdleConnsPerHost,
		MaxConnsPerHost:      defaultMaxConnsPerHost,
		IdleConnTimeout:      defaultIdleConnTimeout,
		TLSHandshakeTimeout:  defaultTLSHandshakeTimeout,
		MaxRedirects:         defaultMaxRedirects,
		DialTimeout:          defaultDialTimeout,
		DialKeepAlive:        defaultDialKeepAlive,
		MaxResponseBodyBytes: defaultMaxResponseBodyBytes,
		DefaultHeaders:       make(map[string]string),
		RetryConfig:          defaultRetryConfig(),
		CircuitBreakerConfig: defaultCircuitBreakerConfig(),
	}
}

// clone returns a deep-enough copy of cfg suitable for independent mutation.
// Slices and maps are copied; pointer sub-configs (RetryConfig, etc.) are
// shared and treated as immutable after construction. CookieJar is
// intentionally shared so that parent and child clients share cookie state.
func (cfg *Config) clone() *Config {
	c := *cfg

	c.DefaultHeaders = make(map[string]string, len(cfg.DefaultHeaders))
	for k, v := range cfg.DefaultHeaders {
		c.DefaultHeaders[k] = v
	}
	c.DNSOverrides = make(map[string]string, len(cfg.DNSOverrides))
	for k, v := range cfg.DNSOverrides {
		c.DNSOverrides[k] = v
	}
	c.TransportMiddlewares = append([]func(http.RoundTripper) http.RoundTripper(nil), cfg.TransportMiddlewares...)
	c.OnBeforeRequest = append([]func(context.Context, *Request) error(nil), cfg.OnBeforeRequest...)
	c.OnAfterResponse = append([]func(context.Context, *Response) error(nil), cfg.OnAfterResponse...)

	return &c
}

// Option is a functional option that mutates a [Config] during [New] or
// [Client.With]. Options are applied left-to-right; later options win.
type Option func(*Config)

// WithBaseURL sets the base URL prepended to every request path that does not
// start with "http://" or "https://".
func WithBaseURL(url string) Option { return func(c *Config) { c.BaseURL = url } }

// WithTimeout sets the end-to-end request timeout (including all retry
// attempts). Use [Request.WithTimeout] for per-request control.
func WithTimeout(d time.Duration) Option { return func(c *Config) { c.Timeout = d } }

// WithConnectionPool tunes the connection pool size.
//
//   - maxIdle: maximum total idle (keep-alive) connections across all hosts.
//   - maxIdlePerHost: maximum idle connections per host.
//   - maxPerHost: maximum total connections per host (0 = unlimited).
func WithConnectionPool(maxIdle, maxIdlePerHost, maxPerHost int) Option {
	return func(c *Config) {
		c.MaxIdleConns = maxIdle
		c.MaxIdleConnsPerHost = maxIdlePerHost
		c.MaxConnsPerHost = maxPerHost
	}
}

// WithIdleConnTimeout sets how long an idle keep-alive connection remains open
// before being evicted from the pool.
func WithIdleConnTimeout(d time.Duration) Option {
	return func(c *Config) { c.IdleConnTimeout = d }
}

// WithResponseHeaderTimeout sets the deadline to read response headers after
// the request body has been sent. 0 disables the timeout.
func WithResponseHeaderTimeout(d time.Duration) Option {
	return func(c *Config) { c.ResponseHeaderTimeout = d }
}

// WithDialTimeout sets the maximum time allowed for the TCP dial to complete.
func WithDialTimeout(d time.Duration) Option { return func(c *Config) { c.DialTimeout = d } }

// WithDialKeepAlive sets the interval between TCP keep-alive probes on active
// connections.
func WithDialKeepAlive(d time.Duration) Option { return func(c *Config) { c.DialKeepAlive = d } }

// WithTLSConfig replaces the default TLS configuration. The default enforces
// TLS 1.2 as the minimum protocol version.
func WithTLSConfig(tlsCfg *tls.Config) Option { return func(c *Config) { c.TLSConfig = tlsCfg } }

// WithProxy sets the proxy URL for all requests. Omit (or pass "") to inherit
// the proxy from the HTTP_PROXY / HTTPS_PROXY environment variables.
func WithProxy(proxyURL string) Option { return func(c *Config) { c.ProxyURL = proxyURL } }

// WithCookieJar sets the cookie jar used by the client. Pass nil to disable
// automatic cookie handling.
func WithCookieJar(jar http.CookieJar) Option { return func(c *Config) { c.CookieJar = jar } }

// WithDefaultCookieJar creates and attaches a standard RFC 6265 cookie jar.
// The jar persists cookies across requests for the lifetime of the client.
func WithDefaultCookieJar() Option {
	return func(c *Config) {
		jar, _ := cookiejar.New(nil)
		c.CookieJar = jar
	}
}

// WithCache attaches a custom [CacheStore] to the client. Use
// [WithInMemoryCache] for the built-in option, or implement [CacheStore] for
// Redis, disk, or other backends.
func WithCache(store CacheStore) Option { return func(c *Config) { c.CacheStore = store } }

// WithInMemoryCache creates and attaches an in-memory LRU cache with the given
// maximum entry count.
func WithInMemoryCache(maxEntries int) Option {
	return func(c *Config) {
		c.CacheStore = NewInMemoryCacheStore(maxEntries)
	}
}

// WithCustomDialer replaces the default [net.Dialer]. When set, the dial
// timeout and keep-alive configured via [WithDialTimeout] / [WithDialKeepAlive]
// are ignored in favor of the dialer's own settings.
func WithCustomDialer(dialer *net.Dialer) Option {
	return func(c *Config) { c.CustomDialer = dialer }
}

// WithDNSOverride maps hostnames to specific IP addresses, bypassing DNS
// resolution for those hosts. Useful for service discovery, split-horizon DNS,
// and integration testing without modifying /etc/hosts.
//
//	WithDNSOverride(map[string]string{"api.internal": "10.0.0.42"})
func WithDNSOverride(hosts map[string]string) Option {
	return func(c *Config) {
		if c.DNSOverrides == nil {
			c.DNSOverrides = make(map[string]string)
		}
		for k, v := range hosts {
			c.DNSOverrides[k] = v
		}
	}
}

// WithRetry replaces the entire retry configuration. Pass nil to restore
// the package defaults (3 attempts, exponential backoff).
func WithRetry(rc *RetryConfig) Option { return func(c *Config) { c.RetryConfig = rc } }

// WithDisableRetry disables all retry behaviour so only a single attempt is
// made.
func WithDisableRetry() Option {
	return func(c *Config) { c.RetryConfig = &RetryConfig{MaxAttempts: 1} }
}

// WithRetryIf sets a custom retry predicate on the active [RetryConfig] (or
// on the default config if none has been set yet). The predicate is evaluated
// when the built-in logic would retry; returning false suppresses the retry.
//
//	WithRetryIf(func(resp *http.Response, err error) bool {
//	    if resp != nil { return resp.StatusCode == 503 }
//	    return true
//	})
func WithRetryIf(fn func(resp *http.Response, err error) bool) Option {
	return func(c *Config) {
		if c.RetryConfig == nil {
			c.RetryConfig = defaultRetryConfig()
		}
		c.RetryConfig.RetryIf = fn
	}
}

// WithOnRetry registers a callback invoked before each retry sleep. Useful
// for structured logging and metrics. attempt is 1-based (first retry = 1).
func WithOnRetry(fn func(attempt int, resp *http.Response, err error)) Option {
	return func(c *Config) {
		if c.RetryConfig == nil {
			c.RetryConfig = defaultRetryConfig()
		}
		c.RetryConfig.OnRetry = fn
	}
}

// WithCircuitBreaker replaces the circuit breaker configuration.
func WithCircuitBreaker(cbc *CircuitBreakerConfig) Option {
	return func(c *Config) { c.CircuitBreakerConfig = cbc }
}

// WithDisableCircuitBreaker removes the circuit breaker entirely so all
// requests are attempted regardless of upstream failure rates.
func WithDisableCircuitBreaker() Option { return func(c *Config) { c.CircuitBreakerConfig = nil } }

// WithOnStateChange registers a callback invoked on every circuit breaker
// state transition. See [CircuitBreakerConfig.OnStateChange] for constraints.
func WithOnStateChange(fn func(from, to CircuitBreakerState)) Option {
	return func(c *Config) {
		if c.CircuitBreakerConfig == nil {
			c.CircuitBreakerConfig = defaultCircuitBreakerConfig()
		}
		c.CircuitBreakerConfig.OnStateChange = fn
	}
}

// WithRateLimit enables the client-side token-bucket rate limiter.
//
//   - rps: sustained request rate in requests per second.
//   - burst: maximum number of tokens that can accumulate above the sustained rate.
func WithRateLimit(rps float64, burst int) Option {
	return func(c *Config) {
		c.RateLimitConfig = &RateLimitConfig{RequestsPerSecond: rps, Burst: burst}
	}
}

// WithDefaultHeaders merges the given headers into every outgoing request.
// Per-request headers always take precedence over these defaults.
func WithDefaultHeaders(headers map[string]string) Option {
	return func(c *Config) {
		for k, v := range headers {
			c.DefaultHeaders[k] = v
		}
	}
}

// WithDisableCompression disables automatic Accept-Encoding negotiation and
// transparent response decompression by the transport.
func WithDisableCompression() Option { return func(c *Config) { c.DisableCompression = true } }

// WithMaxRedirects sets the maximum number of redirects to follow
// automatically. Set to 0 to disable redirect following entirely.
func WithMaxRedirects(n int) Option { return func(c *Config) { c.MaxRedirects = n } }

// WithMaxResponseBodyBytes limits how many bytes of a response body are
// buffered by [Client.Execute]. Responses that exceed this limit are silently
// truncated; check [Response.IsTruncated]. Set to 0 for no limit (default 10 MB).
func WithMaxResponseBodyBytes(n int64) Option {
	return func(c *Config) { c.MaxResponseBodyBytes = n }
}

// WithTransportMiddleware appends one or more [http.RoundTripper] middleware
// functions. Middleware is applied outermost-last — the last appended
// middleware is the first to intercept a request.
func WithTransportMiddleware(mw ...func(http.RoundTripper) http.RoundTripper) Option {
	return func(c *Config) {
		c.TransportMiddlewares = append(c.TransportMiddlewares, mw...)
	}
}

// WithOnBeforeRequest appends a hook called before each request attempt
// (including retries). Returning a non-nil error cancels the request.
func WithOnBeforeRequest(hook func(context.Context, *Request) error) Option {
	return func(c *Config) { c.OnBeforeRequest = append(c.OnBeforeRequest, hook) }
}

// WithOnAfterResponse appends a hook called after each successful response is
// received (after all retries, before returning to the caller). Returning a
// non-nil error propagates as the [Client.Execute] return value.
func WithOnAfterResponse(hook func(context.Context, *Response) error) Option {
	return func(c *Config) { c.OnAfterResponse = append(c.OnAfterResponse, hook) }
}

// WithLogger sets the structured logger used for internal relay events such as
// retries, circuit breaker state changes, and rate-limit waits.
// Use [SlogAdapter], [NewDefaultLogger], or your own implementation.
func WithLogger(l Logger) Option { return func(c *Config) { c.Logger = l } }

// WithCertificatePinning rejects TLS connections whose certificate chain does
// not contain any certificate matching the provided SHA-256 pins. Pins must
// be base64-encoded SHA-256 digests, optionally prefixed with "sha256/".
func WithCertificatePinning(pins []string) Option {
	return func(c *Config) { c.TLSPins = pins }
}

// WithRequestCoalescing enables deduplication of concurrent identical GET and
// HEAD requests. Only one real HTTP call is made; all callers sharing the same
// URL receive independent copies of the response body.
func WithRequestCoalescing() Option { return func(c *Config) { c.EnableCoalescing = true } }

// WithDigestAuth enables HTTP Digest Authentication (RFC 7616). The client
// automatically handles the 401 challenge/response cycle.
func WithDigestAuth(username, password string) Option {
	return func(c *Config) {
		c.DigestUsername = username
		c.DigestPassword = password
	}
}

// WithHARRecording attaches a [HARRecorder] that captures every request and
// response in HAR 1.2 format. Call [HARRecorder.Export] to serialise.
func WithHARRecording(rec *HARRecorder) Option {
	return func(c *Config) { c.HARRecorder = rec }
}

// WithAutoIdempotencyKey automatically injects an X-Idempotency-Key header
// (UUID v4) into every request. The same key is reused across retry attempts,
// preventing duplicate side effects on servers that support idempotency keys.
func WithAutoIdempotencyKey() Option { return func(c *Config) { c.AutoIdempotencyKey = true } }

// WithHealthCheck enables a background health-probe goroutine. While the
// circuit breaker is in the Open state the client periodically issues a GET
// request to url with the given timeout. A response with expectedStatus (or
// any 2xx when expectedStatus is 0) resets the circuit breaker to Closed
// without waiting for the natural ResetTimeout to elapse.
//
// The probe goroutine stops automatically when [Client.Shutdown] is called.
// WithHealthCheck has no effect when the circuit breaker is disabled.
func WithHealthCheck(url string, interval, timeout time.Duration, expectedStatus int) Option {
	return func(c *Config) {
		c.HealthCheck = &HealthCheckConfig{
			URL:            url,
			Interval:       interval,
			Timeout:        timeout,
			ExpectedStatus: expectedStatus,
		}
	}
}

// WithDNSCache enables client-side DNS caching so that each unique hostname
// is resolved at most once per ttl interval. This reduces DNS lookup latency
// on keep-alive-heavy workloads and avoids thundering-herd re-resolution when
// many goroutines dial the same host simultaneously.
//
// The cache is per-client; different [New] calls each maintain their own
// cache. Entries are evicted lazily (on next access) when their TTL expires.
func WithDNSCache(ttl time.Duration) Option {
	return func(c *Config) {
		c.DNSCache = &DNSCacheConfig{TTL: ttl}
	}
}
