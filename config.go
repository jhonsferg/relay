package relay

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
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

// URLNormalisationMode controls how [Config.BaseURL] is resolved against
// request paths in [Request.build]. Each mode has different characteristics
// regarding RFC 3986 compliance, zero-allocation, and API path preservation.
//
// The default mode (NormalisationAuto) provides intelligent automatic detection,
// choosing the best strategy based on whether the base URL appears to be an API
// endpoint with path components.
type URLNormalisationMode int

const (
	// NormalisationAuto (default) uses intelligent detection: RFC 3986 for
	// host-only URLs (e.g., http://api.com), safe string normalisation for
	// APIs with path components (e.g., http://api.com/v1). Zero configuration
	// required; automatically handles versioned APIs, OData, GraphQL, etc.
	NormalisationAuto URLNormalisationMode = iota

	// NormalisationRFC3986 forces RFC 3986 URL resolution via
	// url.ResolveReference(). Provides zero allocations but breaks API URLs
	// with path components (e.g., http://api.com/v1 + Products becomes
	// http://api.com/Products, losing the /v1 segment). Use this only if
	// you have host-only base URLs and need maximum performance.
	NormalisationRFC3986

	// NormalisationAPI forces safe string normalisation for all URLs,
	// preserving base path components by concatenation instead of RFC 3986
	// resolution. Slightly higher allocations than RFC 3986 but works correctly
	// for all API patterns. Use this if you want guaranteed API path preservation.
	NormalisationAPI
)

// String returns a human-readable name for the normalisation mode.
func (m URLNormalisationMode) String() string {
	switch m {
	case NormalisationAuto:
		return "Auto"
	case NormalisationRFC3986:
		return "RFC3986"
	case NormalisationAPI:
		return "API"
	default:
		return "Unknown"
	}
}

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

	// ExpectContinueTimeout is the maximum time to wait for a server's first
	// response headers after fully writing the request headers if the request
	// has an Expect: 100-continue header. Zero means no specific timeout.
	ExpectContinueTimeout time.Duration

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

	// RetryConfig controls retry behaviour. When nil, a sensible default
	// (3 attempts, exponential backoff) is used.
	RetryConfig *RetryConfig

	// RetryBudget limits the fraction of requests that may be retried within a
	// sliding window to prevent retry storms. When nil, no budget is enforced.
	RetryBudget *RetryBudget

	// CircuitBreakerConfig controls the circuit breaker. When nil, a default
	// (5 failures → Open, 60 s reset) is used. Set explicitly to nil with
	// [WithDisableCircuitBreaker] to disable it entirely.
	CircuitBreakerConfig *CircuitBreakerConfig

	// RateLimitConfig enables the client-side token-bucket rate limiter.
	// When nil, rate limiting is disabled.
	RateLimitConfig *RateLimitConfig

	// LoadBalancerConfig distributes requests across multiple backend URLs.
	// When set, BaseURL is ignored and a backend is selected per request.
	// When nil, load balancing is disabled.
	LoadBalancerConfig *LoadBalancerConfig

	// DefaultHeaders are merged into every outgoing request. Per-request headers
	// always take precedence over these defaults.
	DefaultHeaders map[string]string

	// DisableCompression disables automatic Accept-Encoding negotiation and
	// transparent response decompression.
	DisableCompression bool

	// RequestCompression enables transparent gzip compression of request bodies.
	// Requests with no body are not affected. Content-Encoding is set automatically.
	// Set via [WithRequestCompression] or [WithRequestCompressionLevel].
	RequestCompression bool

	// RequestCompressionLevel is the gzip compression level (0-9).
	// A value of 0 means gzip.DefaultCompression is used.
	// Only meaningful when RequestCompression is true.
	RequestCompressionLevel int

	// DisableTiming skips per-request timing instrumentation (httptrace).
	// When true, [Response.Timing] fields are all zero and roughly 10
	// allocations per request are avoided. Useful for high-throughput
	// scenarios where timing metrics are not needed.
	DisableTiming bool

	// MaxRedirects is the maximum number of redirects to follow automatically.
	// Set to 0 to disable redirect following.
	MaxRedirects int

	// MaxResponseBodyBytes is the maximum number of body bytes buffered by
	// [Execute]. Responses exceeding this limit are truncated and
	// [Response.IsTruncated] returns true. Set to 0 for no limit (default 10 MB).
	MaxResponseBodyBytes int64

	// TransportMiddlewares is a chain of [http.RoundTripper] wrappers applied
	// around the core transport. Middleware is applied outermost-last - the last
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

	// ErrorDecoder is called by [Client.Execute] whenever the HTTP response
	// status code is >= 400. It receives the numeric status code and the full,
	// already-buffered response body. When the function returns a non-nil error,
	// [Client.Execute] releases the response and returns that error directly to
	// the caller. When it returns nil the response is returned normally,
	// preserving the default behaviour where HTTP error codes are not
	// automatically treated as errors.
	//
	// ErrorDecoder is invoked after all [OnAfterResponse] hooks have run.
	// Use [WithErrorDecoder] to set it.
	ErrorDecoder func(statusCode int, body []byte) error

	// ResponseDecoder is used by [Response.Decode] and [ExecuteAs] to
	// deserialise response bodies into Go values. When set, it replaces the
	// default encoding/json and encoding/xml decoders. The contentType
	// parameter receives the response Content-Type header (e.g.
	// "application/json", "application/protobuf") so the decoder can pick
	// the right format.
	//
	// Returning a non-nil error from ResponseDecoder propagates as the error
	// from [Response.Decode] or [ExecuteAs].
	//
	// Use [WithResponseDecoder] to set it.
	ResponseDecoder func(contentType string, body []byte, v any) error

	// ResponseValidator is applied after each successful (2xx) response body
	// is read. The body is decoded into an interface{} and passed to Validate.
	// If validation fails, Execute returns a [ValidationError].
	//
	// Use [WithResponseValidator] to set it.
	ResponseValidator SchemaValidator

	// Signer is called for each outgoing HTTP request, after all headers and
	// the idempotency key have been applied, and before the request is sent.
	// Use it to implement request authentication that must inspect and/or
	// mutate the final *http.Request (e.g. HMAC-SHA256, OAuth 1.0a, JWS).
	//
	// Returning a non-nil error from Sign aborts the request attempt and
	// propagates as the [Client.Execute] error.
	//
	// Use [WithSigner] to set it.
	Signer RequestSigner

	// CredentialProvider is called before each outgoing request (including
	// retries) to supply fresh credentials. It runs before [Signer], allowing
	// dynamic tokens (e.g. OAuth, Vault) without rebuilding the client.
	//
	// Use [WithCredentialProvider] to set it.
	CredentialProvider CredentialProvider

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

	// Deduplication configures opt-in singleflight request deduplication.
	// When Enabled, concurrent GET/HEAD requests with the same URL are
	// collapsed into a single real HTTP call. All callers receive their own
	// copy of the response body. Disabled by default.
	Deduplication DeduplicationConfig

	// DigestUsername and DigestPassword enable HTTP Digest Authentication.
	DigestUsername string
	DigestPassword string

	// HARRecorder captures all request/response pairs when non-nil.
	HARRecorder *HARRecorder

	// HTTP2PushHandler is called for each HTTP/2 push-promised response.
	// See [WithHTTP2PushHandler] for details and current limitations.
	HTTP2PushHandler PushPromiseHandler

	// AutoIdempotencyKey automatically injects an X-Idempotency-Key header on
	// every request. The same key is reused across retry attempts.
	AutoIdempotencyKey bool

	// AutoIdempotencyOnSafeRetries is like AutoIdempotencyKey but restricts
	// key injection to HTTP methods that are semantically idempotent or safe:
	// GET, HEAD, PUT, OPTIONS, and TRACE. POST, PATCH, and DELETE are skipped
	// unless the caller sets the key explicitly. The same key is reused across
	// all retry attempts for the request.
	AutoIdempotencyOnSafeRetries bool

	// BeforeRetryHooks are called before each retry sleep, in order.
	// attempt is 1-based. httpResp and err reflect the result that triggered
	// the retry; either may be nil. Use [WithBeforeRetryHook] to append hooks.
	BeforeRetryHooks []BeforeRetryHookFunc

	// BeforeRedirectHooks are called before each redirect is followed.
	// Returning a non-nil error stops the redirect chain; the error propagates
	// as the [Client.Execute] return value. Use [WithBeforeRedirectHook].
	BeforeRedirectHooks []BeforeRedirectHookFunc

	// OnErrorHooks are called when [Client.Execute] returns a non-nil error.
	// They run after all internal error handling and are intended for logging
	// and metrics; the return value is discarded. Use [WithOnErrorHook].
	OnErrorHooks []OnErrorHookFunc

	// HealthCheck enables a background goroutine that proactively probes a
	// health endpoint while the circuit breaker is open and resets it on
	// success. Nil disables the feature.
	HealthCheck *HealthCheckConfig

	// DNSCache enables client-side DNS caching with a configurable TTL.
	// Nil disables the feature (default: OS resolver on every dial).
	DNSCache *DNSCacheConfig

	// URLNormalisationMode controls how [BaseURL] is resolved against request
	// paths. Default is NormalisationAuto, which intelligently detects API URLs
	// and applies the best strategy. Can be overridden with [WithURLNormalisation].
	URLNormalisationMode URLNormalisationMode

	// AutoNormaliseBaseURL automatically adds a trailing slash to BaseURL if
	// missing. Enabled by default for API convenience; can be disabled with
	// [WithAutoNormaliseURL](false).
	AutoNormaliseBaseURL bool

	// parsedBaseURL is a pre-parsed *url.URL for BaseURL (if set).
	// Reduces allocations in request.build() by reusing the parsed URL.
	// Only set by WithBaseURL; never modified after client creation.
	parsedBaseURL *url.URL

	// MaxConcurrentRequests is the maximum number of in-flight requests
	// allowed simultaneously. Zero or negative means no limit.
	MaxConcurrentRequests int

	// EnablePriorityQueue activates priority-aware dequeuing when the bulkhead
	// is at capacity. When enabled, higher-priority requests are dequeued before
	// lower-priority ones. Requests within the same priority level maintain FIFO
	// order. Disabled by default (all requests use PriorityNormal).
	// Only has an effect when MaxConcurrentRequests > 0.
	EnablePriorityQueue bool

	// DefaultAccept is the value sent in the Accept header when no explicit
	// Accept header has been set on the request. Empty string means no default
	// Accept header is added.
	DefaultAccept string

	// UnixSocketPath is the filesystem path of a Unix domain socket to use for
	// all outgoing connections. When set, the TCP dialler is replaced with a
	// Unix socket dialler so that the client communicates with a local server
	// (e.g. the Docker daemon at /var/run/docker.sock) instead of opening a
	// TCP connection. The host in the request URL is still sent as the HTTP
	// Host header; only the transport layer is changed.
	//
	// HTTP/2 is disabled for Unix socket connections because it is not
	// typically negotiated over local sockets.
	//
	// Set via [WithUnixSocket].
	UnixSocketPath string

	// HedgeAfter is the delay before sending a duplicate (hedge) request.
	// Zero disables hedging. When set, a second request is sent after this
	// duration if the first has not completed. The first response wins.
	HedgeAfter time.Duration

	// HedgeMaxAttempts is the maximum number of concurrent hedge requests
	// (including the original). Defaults to 2 when HedgeAfter > 0.
	HedgeMaxAttempts int

	// CertWatcher holds the active dynamic TLS certificate watcher created by
	// [WithDynamicTLSCert] or [WithCertWatcher]. Callers can call Stop() to
	// halt the background reload goroutine.
	CertWatcher *CertWatcher

	// WebSocketDialTimeout is the handshake timeout for [Client.ExecuteWebSocket].
	// Zero uses the client Timeout.
	WebSocketDialTimeout time.Duration

	// SchemeAdapters maps URL schemes to custom [http.RoundTripper] instances.
	// Requests whose scheme matches a key are dispatched to the corresponding
	// transport instead of the default HTTP/HTTPS transport. Set via
	// [WithTransportAdapter].
	SchemeAdapters map[string]http.RoundTripper

	// AdaptiveTimeoutConfig enables adaptive timeout adjustment based on
	// observed response latencies. When set, per-request timeouts are
	// dynamically computed from the percentile of recent latencies.
	// When nil, adaptive timeout is disabled.
	AdaptiveTimeoutConfig *AdaptiveTimeoutConfig
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
		AutoNormaliseBaseURL: true,
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
	c.BeforeRetryHooks = append([]BeforeRetryHookFunc(nil), cfg.BeforeRetryHooks...)
	c.BeforeRedirectHooks = append([]BeforeRedirectHookFunc(nil), cfg.BeforeRedirectHooks...)
	c.OnErrorHooks = append([]OnErrorHookFunc(nil), cfg.OnErrorHooks...)

	if cfg.SchemeAdapters != nil {
		c.SchemeAdapters = make(map[string]http.RoundTripper, len(cfg.SchemeAdapters))
		for k, v := range cfg.SchemeAdapters {
			c.SchemeAdapters[k] = v
		}
	}

	return &c
}

// NormaliseBaseURL ensures a base URL ends with a trailing slash if it has a
// non-empty, non-root path. For host-only URLs (e.g., "http://api.com"), the
// slash is only added if the URL doesn't already end with one. This prevents
// the common mistake of losing path components during URL resolution.
//
// Examples:
//   - "http://api.com" → "http://api.com/"
//   - "http://api.com/v1" → "http://api.com/v1/"
//   - "http://api.com/v1/" → "http://api.com/v1/"
//   - "" → ""
func NormaliseBaseURL(urlStr string) string {
	if urlStr == "" {
		return ""
	}

	// If it already ends with /, no change needed
	if strings.HasSuffix(urlStr, "/") {
		return urlStr
	}

	// Add trailing slash
	return urlStr + "/"
}

// isAPIBase detects whether a base URL represents an API endpoint with path
// components, which require safe string normalisation instead of RFC 3986
// resolution. Returns true if the path contains common API patterns or has
// non-trivial path segments.
//
// Examples:
//   - "http://api.example.com/v1" → true (has /v1)
//   - "http://api.example.com/odata" → true (has /odata)
//   - "http://example.com" → false (host-only)
//   - "http://example.com/" → false (only trailing slash)
func isAPIBase(baseURL string) bool {
	if baseURL == "" {
		return false
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return false
	}

	path := parsed.Path
	if path == "" || path == "/" {
		return false
	}

	// Check common API path patterns (zero-alloc: direct string prefix checks)
	// Common patterns: /api, /v1, /v2, /odata, /rest, /graphql, /sap, etc.
	if strings.HasPrefix(path, "/api") ||
		strings.HasPrefix(path, "/v1") || strings.HasPrefix(path, "/v2") ||
		strings.HasPrefix(path, "/v3") || strings.HasPrefix(path, "/v4") ||
		strings.HasPrefix(path, "/v5") || strings.HasPrefix(path, "/odata") ||
		strings.HasPrefix(path, "/rest") || strings.HasPrefix(path, "/graphql") ||
		strings.HasPrefix(path, "/soap") || strings.HasPrefix(path, "/sap") ||
		strings.HasPrefix(path, "/data") || strings.HasPrefix(path, "/service") ||
		strings.HasPrefix(path, "/services") {
		return true
	}

	// Also return true if path has 2+ segments (e.g. /company/api, /service/v1)
	// Count slashes to detect depth; more than one slash means multiple segments
	slashCount := 0
	for _, c := range path {
		if c == '/' {
			slashCount++
		}
	}
	return slashCount > 1
}

// Option is a functional option that mutates a [Config] during [New] or
// [Client.With]. Options are applied left-to-right; later options win.
type Option func(*Config)

// WithBaseURL sets the base URL prepended to every request path that does not
// start with "http://" or "https://". The URL is pre-parsed once for
// performance. If [Config.AutoNormaliseBaseURL] is true (the default),
// a trailing slash is automatically added if missing.
func WithBaseURL(urlStr string) Option {
	return func(c *Config) {
		if c.AutoNormaliseBaseURL {
			urlStr = NormaliseBaseURL(urlStr)
		}
		c.BaseURL = urlStr
		if urlStr != "" {
			if parsed, err := url.Parse(urlStr); err == nil {
				c.parsedBaseURL = parsed
			}
		}
	}
}

// WithURLNormalisation sets the URL normalisation strategy for resolving base
// URLs against request paths. The default is NormalisationAuto, which
// intelligently detects API URLs and chooses the best strategy.
//
// Modes:
//   - NormalisationAuto (default): Detects API patterns and uses appropriate
//     strategy (RFC 3986 for host-only, safe normalisation for APIs).
//   - NormalisationRFC3986: Forces RFC 3986 resolution (zero-alloc, breaks APIs).
//   - NormalisationAPI: Forces safe string normalisation (preserves all paths).
func WithURLNormalisation(mode URLNormalisationMode) Option {
	return func(c *Config) {
		c.URLNormalisationMode = mode
	}
}

// WithAutoNormaliseURL enables or disables automatic trailing slash
// normalisation for base URLs. Enabled by default. When disabled,
// [WithBaseURL] passes the URL as-is without modification.
func WithAutoNormaliseURL(enable bool) Option {
	return func(c *Config) {
		c.AutoNormaliseBaseURL = enable
	}
}

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
// are ignored in favour of the dialer's own settings.
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

// WithRetryBudget sets a sliding-window retry budget that caps the fraction of
// requests that may be retried within the window. This prevents retry storms
// when a downstream service degrades. See [RetryBudget] for field semantics.
func WithRetryBudget(b *RetryBudget) Option { return func(c *Config) { c.RetryBudget = b } }

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

// WithLoadBalancer distributes requests across multiple backend URLs using the
// given strategy. When set, BaseURL is ignored and each request selects a backend
// from cfg.Backends. Use [RoundRobin] (default) or [Random] as strategy.
func WithLoadBalancer(cfg LoadBalancerConfig) Option {
	return func(c *Config) {
		c.LoadBalancerConfig = &cfg
	}
}

// WithDefaultHeaders merges the given headers into every outgoing request.
// Per-request headers always take precedence over these defaults.
// Header values are sanitized to strip CR/LF characters.
func WithDefaultHeaders(headers map[string]string) Option {
	return func(c *Config) {
		for k, v := range headers {
			c.DefaultHeaders[k] = sanitizeHeaderValue(v)
		}
	}
}

// WithDisableCompression disables automatic Accept-Encoding negotiation and
// transparent response decompression by the transport.
func WithDisableCompression() Option { return func(c *Config) { c.DisableCompression = true } }

// WithDisableTiming skips per-request timing instrumentation.
// When set, [Response.Timing] fields are all zero and approximately 10
// allocations per [Client.Execute] call are avoided. Recommended for
// high-throughput scenarios where timing metrics are not required.
func WithDisableTiming() Option { return func(c *Config) { c.DisableTiming = true } }

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
// functions. Middleware is applied outermost-last - the last appended
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

// WithErrorDecoder sets a function that translates HTTP error status codes
// (>= 400) into typed Go errors. The function receives the numeric status code
// and the fully-buffered response body. When it returns a non-nil error,
// [Client.Execute] releases the response and returns that error to the caller.
// When it returns nil the response is returned unchanged, preserving the
// default behaviour where HTTP error codes are not automatically errors.
//
// The decoder runs after all [WithOnAfterResponse] hooks.
//
// Example - map 404 to a sentinel:
//
//	var ErrNotFound = errors.New("not found")
//
//	client := relay.New(
//	    relay.WithErrorDecoder(func(status int, body []byte) error {
//	        if status == http.StatusNotFound {
//	            return ErrNotFound
//	        }
//	        return nil
//	    }),
//	)
func WithErrorDecoder(fn func(statusCode int, body []byte) error) Option {
	return func(c *Config) { c.ErrorDecoder = fn }
}

// WithResponseDecoder sets a custom deserialiser used by [Response.Decode] and
// [ExecuteAs]. When configured it replaces the built-in encoding/json and
// encoding/xml decoders. The contentType parameter receives the response
// Content-Type header value so the function can select the appropriate format.
//
// Returning a non-nil error propagates as the error from [Response.Decode] or
// [ExecuteAs].
//
// Example - use Protocol Buffers when the server sends application/protobuf:
//
//	client := relay.New(
//	    relay.WithResponseDecoder(func(ct string, body []byte, v any) error {
//	        if strings.Contains(ct, "protobuf") {
//	            return proto.Unmarshal(body, v.(proto.Message))
//	        }
//	        return json.Unmarshal(body, v) // fallback to JSON
//	    }),
//	)
func WithResponseDecoder(fn func(contentType string, body []byte, v any) error) Option {
	return func(c *Config) { c.ResponseDecoder = fn }
}

// WithResponseValidator sets a [SchemaValidator] that is applied after each
// successful (2xx) response is decoded. If validation fails, Execute returns
// a [ValidationError] wrapping the validation failure details.
func WithResponseValidator(v SchemaValidator) Option {
	return func(c *Config) { c.ResponseValidator = v }
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

// WithRequestDeduplication enables singleflight-based deduplication for GET
// and HEAD requests. Concurrent requests to the same URL are collapsed into a
// single real HTTP call; all callers receive their own copy of the response.
// Disabled by default. Use per-request WithDeduplication to override.
func WithRequestDeduplication() Option {
	return func(c *Config) { c.Deduplication.Enabled = true }
}

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

// WithHARRecorder is an alias for [WithHARRecording].
func WithHARRecorder(rec *HARRecorder) Option {
	return WithHARRecording(rec)
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

// --- Semantic hook types -----------------------------------------------------

// BeforeRetryHookFunc is called before each retry sleep. attempt is 1-based
// (first retry = 1). req is the original relay [Request]. httpResp and err
// reflect the result that triggered the retry; either may be nil.
type BeforeRetryHookFunc func(ctx context.Context, attempt int, req *Request, httpResp *http.Response, err error)

// BeforeRedirectHookFunc is called before each redirect is followed. Returning
// a non-nil error stops the redirect chain and propagates as the
// [Client.Execute] return value.
type BeforeRedirectHookFunc func(req *http.Request, via []*http.Request) error

// OnErrorHookFunc is called when [Client.Execute] returns a non-nil error.
// It is intended for logging and metrics; its return value is discarded.
type OnErrorHookFunc func(ctx context.Context, req *Request, err error)

// WithBeforeRetryHook appends a hook invoked before each retry sleep.
// Multiple hooks are called in the order they are registered.
//
//	WithBeforeRetryHook(func(ctx context.Context, attempt int, req *relay.Request, httpResp *http.Response, err error) {
//	   slog.InfoContext(ctx, "retrying", "attempt", attempt, "err", err)
//	})
func WithBeforeRetryHook(fn BeforeRetryHookFunc) Option {
	return func(c *Config) { c.BeforeRetryHooks = append(c.BeforeRetryHooks, fn) }
}

// WithBeforeRedirectHook appends a hook invoked before each redirect.
// Returning a non-nil error aborts the redirect chain.
//
//	WithBeforeRedirectHook(func(req *http.Request, via []*http.Request) error {
//	   if len(via) > 3 { return errors.New("too many redirects") }
//	   return nil
//	})
func WithBeforeRedirectHook(fn BeforeRedirectHookFunc) Option {
	return func(c *Config) { c.BeforeRedirectHooks = append(c.BeforeRedirectHooks, fn) }
}

// WithOnErrorHook appends a hook invoked whenever [Client.Execute] returns a
// non-nil error. Use it for structured error logging or metrics.
//
//	WithOnErrorHook(func(ctx context.Context, req *relay.Request, err error) {
//	   slog.ErrorContext(ctx, "request failed", "method", req.Method(), "err", err)
//	})
func WithOnErrorHook(fn OnErrorHookFunc) Option {
	return func(c *Config) { c.OnErrorHooks = append(c.OnErrorHooks, fn) }
}

// WithAutoIdempotencyOnSafeRetries automatically injects an X-Idempotency-Key
// header for HTTP methods that are semantically idempotent or safe (GET, HEAD,
// PUT, OPTIONS, TRACE). POST, PATCH, and DELETE are skipped unless the caller
// sets a key explicitly. The same key is reused across all retry attempts for
// a given request.
//
// Use [WithAutoIdempotencyKey] instead if you want to inject the key for all
// methods unconditionally.
func WithAutoIdempotencyOnSafeRetries() Option {
	return func(c *Config) { c.AutoIdempotencyOnSafeRetries = true }
}

// WithTLSHandshakeTimeout sets the deadline for completing the TLS handshake.
func WithTLSHandshakeTimeout(d time.Duration) Option {
	return func(c *Config) { c.TLSHandshakeTimeout = d }
}

// WithExpectContinueTimeout sets the maximum time to wait for a server's
// first response headers when the request has an Expect: 100-continue header.
// Zero disables the timeout.
func WithExpectContinueTimeout(d time.Duration) Option {
	return func(c *Config) { c.ExpectContinueTimeout = d }
}

// WithMaxConcurrentRequests sets the maximum number of in-flight requests
// allowed simultaneously (bulkhead). Zero or negative means no limit.
func WithMaxConcurrentRequests(n int) Option {
	return func(c *Config) { c.MaxConcurrentRequests = n }
}

// WithPriorityQueue enables priority-aware request dequeuing when the bulkhead
// is at capacity. Must be used with [WithMaxConcurrentRequests]. When enabled,
// higher-priority requests are dequeued before lower-priority ones. Requests
// within the same priority level maintain FIFO order. Disabled by default.
func WithPriorityQueue() Option {
	return func(c *Config) { c.EnablePriorityQueue = true }
}

// WithDefaultAccept sets the default Accept header sent when the request does
// not already carry one. Empty string disables the default.
func WithDefaultAccept(accept string) Option {
	return func(c *Config) { c.DefaultAccept = accept }
}

// WithHedging enables request hedging: a duplicate request is sent after d if
// the first has not completed. The first response wins; the other is cancelled.
// Defaults to 2 concurrent attempts.
func WithHedging(after time.Duration) Option {
	return func(c *Config) { c.HedgeAfter = after }
}

// WithHedgingN enables request hedging with up to maxAttempts concurrent
// duplicates. after is the delay between launching each attempt.
func WithHedgingN(after time.Duration, maxAttempts int) Option {
	return func(c *Config) {
		c.HedgeAfter = after
		c.HedgeMaxAttempts = maxAttempts
	}
}

// WithWebSocketDialTimeout sets the handshake timeout used by
// [Client.ExecuteWebSocket]. Zero (the default) falls back to the client
// [Config.Timeout].
func WithWebSocketDialTimeout(d time.Duration) Option {
	return func(c *Config) { c.WebSocketDialTimeout = d }
}

// WithTransportAdapter registers a custom [http.RoundTripper] for requests
// whose URL scheme matches scheme (e.g. "myproto", "grpc", "ftp"). When the
// scheme router encounters a request with that scheme, it dispatches to rt
// instead of the default transport. "http" and "https" cannot be overridden
// with this option; use [WithTransportMiddleware] for those.
func WithTransportAdapter(scheme string, rt http.RoundTripper) Option {
	return func(c *Config) {
		if c.SchemeAdapters == nil {
			c.SchemeAdapters = make(map[string]http.RoundTripper)
		}
		c.SchemeAdapters[scheme] = rt
	}
}

// WithAdaptiveTimeout enables adaptive timeout adjustment based on observed
// response latencies. The client tracks recent response times and computes
// per-request timeouts as a percentile of that data, multiplied by a factor.
// When disabled (nil), all requests use the fixed client timeout.
func WithAdaptiveTimeout(cfg AdaptiveTimeoutConfig) Option {
	return func(c *Config) { c.AdaptiveTimeoutConfig = &cfg }
}
