package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// contextKey is a private type for context values managed by this package.
// Using a named type avoids collisions with keys from other packages.
type contextKey int

// redirectCountKey is the context key used to pass the redirect counter from
// the CheckRedirect policy back to Execute so it can populate
// [Response.RedirectCount].
const redirectCountKey contextKey = 0

// redirectChainKey is the context key used to pass the redirect chain from
// the CheckRedirect policy back to Execute so it can populate
// [Response.RedirectChain].
const redirectChainKey contextKey = 1

// Client is a production-grade HTTP client with a configurable transport stack,
// automatic retry/backoff, circuit breaker, token-bucket rate limiter, HTTP
// response caching, OpenTelemetry distributed tracing and metrics, streaming,
// batch, and async execution.
//
// A zero-value Client is not usable; always construct via [New] or [Client.With].
// The client is safe for concurrent use by multiple goroutines.
type Client struct {
	// closed is set to true by [Shutdown]. New Execute calls check this flag
	// and return [ErrClientClosed] immediately without sending a request.
	// Placed first as it's checked on every Execute (hot).
	closed atomic.Bool
	// Padding to isolate closed to its own cache line (64 bytes on most x64)
	_ [63]byte

	// httpClient is the underlying standard-library client that owns the
	// transport stack, redirect policy, timeout, and cookie jar.
	httpClient *http.Client

	// config is the finalised configuration used to build this client.
	// It must not be mutated after construction.
	config *Config

	// circuitBreaker guards downstream calls. Nil when disabled.
	circuitBreaker *CircuitBreaker

	// retrier executes the retry loop around each HTTP call.
	retrier *retrier

	// retryBudgetTracker enforces the sliding-window retry budget. Nil when disabled.
	retryBudgetTracker *retryBudgetTracker

	// rateLimiter enforces the client-side request rate. Nil when disabled.
	rateLimiter *tokenBucket

	// loadBalancer distributes requests across multiple backends. Nil when disabled.
	loadBalancer *loadBalancer

	// adaptiveTimeout tracks observed response latencies and computes
	// dynamic per-request timeouts. Nil when adaptive timeout is disabled.
	adaptiveTimeout *adaptiveTimeoutTracker

	// inFlight tracks requests that are currently in progress so that
	// [Shutdown] can wait for them to complete before closing the pool.
	inFlight sync.WaitGroup

	// bgCancel cancels the context shared by all background goroutines
	// (health check, etc.). Called by Shutdown to stop them gracefully.
	bgCancel context.CancelFunc

	// bulkhead limits the maximum number of concurrent in-flight requests.
	// Nil means unlimited.
	bulkhead chan struct{}

	// priorityQueue manages request ordering when enabled and the bulkhead is
	// at capacity. Nil when priority queue is disabled or bulkhead is disabled.
	priorityQueue *priorityQueue
}

// New creates a Client from the provided functional options. Options are applied
// on top of sensible defaults (30 s timeout, 3-attempt exponential backoff,
// 5-failure circuit breaker). The client is immutable after construction; use
// [Client.With] to derive variants with different settings.
//
//	client := httpclient.New(
//	    httpclient.WithBaseURL("https://api.example.com"),
//	    httpclient.WithTimeout(10*time.Second),
//	)
func New(opts ...Option) *Client {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	return buildClient(cfg)
}

// With returns a new Client that inherits all settings from the current client
// and applies the given options on top. The original client is not modified.
//
// Cookie jars are intentionally shared between parent and child (same session).
// Pass WithCookieJar(nil) in the child to detach.
func (c *Client) With(opts ...Option) *Client {
	cfg := c.config.clone()
	for _, opt := range opts {
		opt(cfg)
	}
	return buildClient(cfg)
}

// buildClient constructs the full client stack from a finalised [Config].
//
// Transport stack (innermost → outermost):
//
//	HTTP Transport → TLS Pinning (via TLSConfig) → Cache →
//	Digest Auth → Coalescing → HAR → External Middlewares
func buildClient(cfg *Config) *Client {
	// Ensure a logger is always set.
	if cfg.Logger == nil {
		cfg.Logger = NoopLogger()
	}

	// Apply TLS certificate pinning to the TLS config before building transport.
	if len(cfg.TLSPins) > 0 {
		pinnedTLS, err := buildTLSConfigWithPinning(cfg.TLSConfig, cfg.TLSPins)
		if err == nil {
			cfg.TLSConfig = pinnedTLS
		}
	}

	transport := buildTransport(cfg)

	if len(cfg.SchemeAdapters) > 0 {
		transport = &schemeRouter{adapters: cfg.SchemeAdapters, fallback: transport}
	}

	if cfg.CacheStore != nil {
		transport = newCachingTransport(transport, cfg.CacheStore)
	}

	if cfg.DigestUsername != "" {
		transport = newDigestTransport(transport, cfg.DigestUsername, cfg.DigestPassword)
	}

	if cfg.EnableCoalescing {
		transport = newCoalesceTransport(transport)
	}

	if cfg.Deduplication.Enabled {
		transport = newDeduplicator(transport)
	}

	if cfg.HARRecorder != nil {
		transport = newHARTransport(transport, cfg.HARRecorder)
	}

	for i := len(cfg.TransportMiddlewares) - 1; i >= 0; i-- {
		transport = cfg.TransportMiddlewares[i](transport)
	}

	// CheckRedirect writes the redirect count into the per-request context so
	// Execute can report it on the Response.
	redirectPolicy := func(req *http.Request, via []*http.Request) error {
		if countPtr, ok := req.Context().Value(redirectCountKey).(*int); ok {
			*countPtr = len(via)
		}
		if chainPtr, ok := req.Context().Value(redirectChainKey).(*[]RedirectInfo); ok {
			var statusCode int
			if req.Response != nil {
				statusCode = req.Response.StatusCode
			}
			var from string
			if len(via) > 0 {
				from = via[len(via)-1].URL.String()
			}
			*chainPtr = append(*chainPtr, RedirectInfo{
				From:       from,
				To:         req.URL.String(),
				StatusCode: statusCode,
			})
		}
		if len(via) >= cfg.MaxRedirects {
			return fmt.Errorf("stopped after %d redirects", cfg.MaxRedirects)
		}
		for _, hook := range cfg.BeforeRedirectHooks {
			if hookErr := hook(req, via); hookErr != nil {
				return hookErr
			}
		}
		return nil
	}

	httpClient := &http.Client{
		Transport:     transport,
		Timeout:       cfg.Timeout,
		CheckRedirect: redirectPolicy,
		Jar:           cfg.CookieJar,
	}

	bgCtx, bgCancel := context.WithCancel(context.Background()) //nolint:gosec // G118: bgCancel is stored in Client and called in Shutdown

	c := &Client{
		httpClient:     httpClient,
		config:         cfg,
		circuitBreaker: newCircuitBreaker(cfg.CircuitBreakerConfig),
		retrier:        newRetrier(cfg.RetryConfig),
		bgCancel:       bgCancel,
	}

	if cfg.RetryBudget != nil {
		c.retryBudgetTracker = newRetryBudgetTracker(*cfg.RetryBudget)
	}

	if cfg.RateLimitConfig != nil {
		c.rateLimiter = newTokenBucket(
			cfg.RateLimitConfig.RequestsPerSecond,
			cfg.RateLimitConfig.Burst,
		)
	}

	if cfg.LoadBalancerConfig != nil {
		c.loadBalancer = newLoadBalancer(cfg.LoadBalancerConfig)
	}

	if cfg.AdaptiveTimeoutConfig != nil {
		c.adaptiveTimeout = newAdaptiveTimeoutTracker(cfg.AdaptiveTimeoutConfig)
	}

	if cfg.MaxConcurrentRequests > 0 {
		c.bulkhead = make(chan struct{}, cfg.MaxConcurrentRequests)
		if cfg.EnablePriorityQueue {
			c.priorityQueue = newPriorityQueue()
		}
	}

	if cfg.HealthCheck != nil && cfg.CircuitBreakerConfig != nil {
		go c.runHealthCheck(bgCtx, cfg.HealthCheck)
	}

	return c
}

// acquireBulkhead acquires a bulkhead slot, using priority queue ordering if enabled.
// Returns a release function that must be called to release the slot.
// If ctx is cancelled, returns an error without acquiring.
func (c *Client) acquireBulkhead(ctx context.Context, req *Request) (func(), error) {
	if c.bulkhead == nil {
		// No bulkhead, return no-op release
		return func() {}, nil
	}

	if c.priorityQueue == nil {
		// No priority queue, use simple channel acquisition
		select {
		case c.bulkhead <- struct{}{}:
			return func() { <-c.bulkhead }, nil
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: %w", ErrBulkheadFull, ctx.Err())
		}
	}

	// pqRelease releases the bulkhead slot and wakes the highest-priority
	// waiter in the priority queue (if any) so it can acquire the freed slot.
	pqRelease := func() {
		<-c.bulkhead
		c.priorityQueue.DequeueNext()
	}

	// Priority queue enabled: try to acquire immediately, otherwise queue
	select {
	case c.bulkhead <- struct{}{}:
		// Got slot immediately - use priority-aware release so waiting
		// requests are woken in priority order when this slot frees up.
		return pqRelease, nil
	default:
		// Bulkhead full, queue the request with priority and wait
		priority := req.priority
		if priority == 0 {
			priority = PriorityNormal
		}
		if err := c.priorityQueue.EnqueueAndWait(ctx, req, priority); err != nil {
			return nil, err
		}
		// After being dequeued (woken by a pqRelease call), acquire the
		// bulkhead slot that was just freed.
		select {
		case c.bulkhead <- struct{}{}:
			return pqRelease, nil
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: %w", ErrBulkheadFull, ctx.Err())
		}
	}
}

// Execute sends the request through the full pipeline:
//
//	closed-guard -> ctx guard -> OnBeforeRequest hooks -> rate limiter ->
//	circuit breaker -> retrier -> transport stack -> OnAfterResponse hooks
//
// A non-nil error is returned for transport-level failures, context
// cancellations, and when the circuit breaker is open. HTTP error status codes
// (4xx, 5xx) are NOT converted to errors - inspect [Response.IsError] or call
// [Response.AsHTTPError] to handle them.
func (c *Client) Execute(req *Request) (resp *Response, err error) {
	if req == nil {
		return nil, ErrNilRequest
	}
	if c.closed.Load() {
		return nil, ErrClientClosed
	}

	c.inFlight.Add(1)
	defer c.inFlight.Done()

	ctx := req.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var hasRequestTimeout bool
	var cancel context.CancelFunc

	// Apply per-request timeout first so hooks and the rate limiter respect it.
	if req.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, req.timeout)
		defer cancel()
		hasRequestTimeout = true
	}

	// Clone request with the timeout context so hooks see an up-to-date ctx.
	// The redirect counter and httptrace context are injected below, after the
	// circuit breaker check, to avoid allocating them on rejected requests.
	req = req.withCtx(ctx)

	// Auto-generate idempotency key once per request (reused across retries).
	if req.idempotencyKey == "" {
		if c.config.AutoIdempotencyKey ||
			(c.config.AutoIdempotencyOnSafeRetries && isSafeMethod(req.method)) {
			if key, genErr := generateIdempotencyKey(); genErr == nil {
				req.idempotencyKey = key
			}
		}
	}

	for _, hook := range c.config.OnBeforeRequest {
		if hookErr := hook(ctx, req); hookErr != nil {
			return nil, fmt.Errorf("OnBeforeRequest: %w", hookErr)
		}
	}

	// Apply default Accept header when not already set on the request (F4).
	if c.config.DefaultAccept != "" && req.headers["Accept"] == "" {
		req = req.WithHeader("Accept", c.config.DefaultAccept)
	}

	if c.rateLimiter != nil {
		if waitErr := c.rateLimiter.Wait(ctx); waitErr != nil {
			return nil, waitErr
		}
	}

	if c.circuitBreaker != nil && !c.circuitBreaker.Allow() {
		return nil, ErrCircuitOpen
	}

	// Acquire a bulkhead slot before entering the retry loop so retries do
	// not consume additional slots (F2).
	releaseBulkhead, err := c.acquireBulkhead(ctx, req)
	if err != nil {
		return nil, err
	}
	defer releaseBulkhead()

	if c.config.HedgeAfter > 0 {
		return c.executeHedged(ctx, req, c.config.HedgeAfter, c.config.HedgeMaxAttempts)
	}

	return c.executeOnce(ctx, req, hasRequestTimeout)
}

// executeOnce performs a single full request attempt (including retries)
// from after the circuit breaker and bulkhead checks. It is called by
// Execute for the normal path and by executeHedged for each hedge attempt.
func (c *Client) executeOnce(ctx context.Context, req *Request, hasRequestTimeout bool) (*Response, error) {
	// Embed a redirect counter and chain so CheckRedirect can populate them.
	var redirectCount int
	var redirectChain []RedirectInfo
	ctx = context.WithValue(ctx, redirectCountKey, &redirectCount)
	ctx = context.WithValue(ctx, redirectChainKey, &redirectChain)

	// Inject httptrace for request timing unless the caller has disabled it.
	var timingCol *timingCollector
	if !c.config.DisableTiming {
		ctx, timingCol = injectTraceContext(ctx)
	}

	// Update the cloned request's stored context in-place (zero allocation)
	// to carry the redirect counter and timing trace for req.build().
	req.ctx = ctx

	// Apply adaptive timeout if configured and no per-request timeout is set.
	if c.adaptiveTimeout != nil && !hasRequestTimeout {
		adaptiveTimeoutDur := c.adaptiveTimeout.computeTimeout()
		if adaptiveTimeoutDur > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, adaptiveTimeoutDur)
			defer cancel()
			hasRequestTimeout = true
			req.ctx = ctx
		}
	}

	var httpResp *http.Response
	var err error
	// If BeforeRetryHooks are registered, compose them with any existing
	// RetryConfig.OnRetry into a per-Execute retrier (thread-safe copy).
	activeRetrier := c.retrier
	if len(c.config.BeforeRetryHooks) > 0 {
		cfgCopy := *c.retrier.cfg
		origOnRetry := cfgCopy.OnRetry
		hooks := c.config.BeforeRetryHooks // capture slice, not c.config
		cfgCopy.OnRetry = func(attempt int, httpR *http.Response, retryErr error) {
			if origOnRetry != nil {
				origOnRetry(attempt, httpR, retryErr)
			}
			for _, hook := range hooks {
				hook(ctx, attempt, req, httpR, retryErr)
			}
		}
		activeRetrier = &retrier{cfg: &cfgCopy, budget: c.retryBudgetTracker}
	} else if c.retryBudgetTracker != nil {
		// Attach budget to a shallow copy so the base retrier stays immutable.
		cfgCopy := *c.retrier.cfg
		activeRetrier = &retrier{cfg: &cfgCopy, budget: c.retryBudgetTracker}
	}
	httpResp, err = activeRetrier.Do(ctx, func() (*http.Response, error) {
		// Select load-balanced backend if configured; falls back to BaseURL.
		baseURL := c.config.BaseURL
		parsedBaseURL := c.config.parsedBaseURL
		if c.loadBalancer != nil {
			backend, lbErr := c.loadBalancer.selectBackend()
			if lbErr != nil {
				return nil, lbErr
			}
			baseURL = backend
			parsedBackend, parseErr := url.Parse(backend)
			if parseErr != nil {
				return nil, fmt.Errorf("load balancer: invalid backend URL %q: %w", backend, parseErr)
			}
			parsedBaseURL = parsedBackend
		}
		httpReq, buildErr := req.build(baseURL, parsedBaseURL, c.config.URLNormalisationMode)
		if buildErr != nil {
			return nil, buildErr
		}
		for k, v := range c.config.DefaultHeaders {
			if httpReq.Header.Get(k) == "" {
				httpReq.Header.Set(k, v)
			}
		}
		// Inject idempotency key (same key on all retries).
		if req.idempotencyKey != "" {
			httpReq.Header.Set(idempotencyKeyHeader, req.idempotencyKey)
		}
		// Sign the request if a signer is configured.
		if c.config.Signer != nil {
			if signErr := c.config.Signer.Sign(httpReq); signErr != nil {
				return nil, fmt.Errorf("request signer: %w", signErr)
			}
		}
		resp, doErr := c.httpClient.Do(httpReq)
		// Always release the pooled reader after Do returns.
		req.releasePooledReader()
		return resp, doErr
	})

	if err != nil {
		// Do not penalise the circuit breaker for redirect-policy stops: those
		// are configuration-level decisions, not downstream failures. A *url.Error
		// whose Err does not contain a network-level cause indicates a redirect
		// stop or similar policy error.
		if c.circuitBreaker != nil && !isRedirectError(err) {
			c.circuitBreaker.RecordFailure()
		}
		if hasRequestTimeout && errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("%w: %w", ErrTimeout, err)
		}
		for _, hook := range c.config.OnErrorHooks {
			hook(ctx, req, err)
		}
		return nil, err
	}

	if c.circuitBreaker != nil {
		if httpResp.StatusCode >= 500 {
			c.circuitBreaker.RecordFailure()
		} else {
			c.circuitBreaker.RecordSuccess()
		}
	}

	// Wrap the response body with a download-progress reader if requested.
	if req.downloadProgress != nil {
		var total int64 = -1
		if httpResp.ContentLength > 0 {
			total = httpResp.ContentLength
		}
		httpResp.Body = newProgressReadCloser(httpResp.Body, total, req.downloadProgress)
	}

	// Read the body before computing totalDur so ContentTransfer includes
	// the actual body download time and not just the time-to-first-byte.
	maxBody := c.config.MaxResponseBodyBytes
	if req.maxBodyBytes != 0 {
		maxBody = req.maxBodyBytes
	}
	resp, err := newResponse(httpResp, maxBody, redirectCount, redirectChain)
	if err != nil {
		return nil, err
	}
	resp.decode = c.config.ResponseDecoder
	if timingCol != nil {
		startNano := timingCol.requestStart.Load()
		totalNano := nowNano() - startNano
		resp.Timing = buildTiming(timingCol, time.Duration(totalNano))

		// Record the response latency for adaptive timeout if configured.
		if c.adaptiveTimeout != nil && resp.Timing.Total > 0 {
			c.adaptiveTimeout.record(resp.Timing.Total)
		}
	}

	for _, hook := range c.config.OnAfterResponse {
		if hookErr := hook(ctx, resp); hookErr != nil {
			return nil, fmt.Errorf("OnAfterResponse: %w", hookErr)
		}
	}

	// Translate HTTP error codes into typed Go errors when a decoder is set.
	// Runs after all OnAfterResponse hooks so hooks see the raw response first.
	if c.config.ErrorDecoder != nil && resp.StatusCode >= 400 {
		if decErr := c.config.ErrorDecoder(resp.StatusCode, resp.Body()); decErr != nil {
			PutResponse(resp)
			return nil, decErr
		}
	}

	// Apply response schema validation on successful (2xx) responses.
	if c.config.ResponseValidator != nil && resp.IsSuccess() {
		var decoded interface{}
		if jsonErr := json.Unmarshal(resp.Body(), &decoded); jsonErr != nil {
			return nil, &ValidationError{Message: fmt.Sprintf("cannot decode response for validation: %s", jsonErr)}
		}
		if valErr := c.config.ResponseValidator.Validate(decoded); valErr != nil {
			return nil, valErr
		}
	}

	return resp, nil
}

// ExecuteJSON is a convenience wrapper that calls [Execute] and, on a
// successful (2xx) response, unmarshals the body into out via encoding/json.
// If out is nil the unmarshal step is skipped. The [Response] is always
// returned alongside any unmarshal error.
//
//	var order Order
//	resp, err := client.ExecuteJSON(
//	    client.Post("/orders").WithJSON(payload),
//	    &order,
//	)
func (c *Client) ExecuteJSON(req *Request, out interface{}) (*Response, error) {
	resp, err := c.Execute(req)
	if err != nil {
		return nil, err
	}
	if out != nil && resp.IsSuccess() {
		if jsonErr := json.Unmarshal(resp.Body(), out); jsonErr != nil {
			return resp, fmt.Errorf("decode response: %w", jsonErr)
		}
	}
	return resp, nil
}

// Shutdown gracefully stops the client. It marks the client as closed (new
// [Execute] calls immediately return [ErrClientClosed]), cancels all background
// goroutines (health check, etc.), waits for all in-flight requests - including
// open streaming bodies - to finish, then closes idle connections in the pool.
//
// If ctx expires before the drain completes, Shutdown returns ctx.Err() but
// does NOT forcefully abort in-flight requests - their own contexts govern that.
func (c *Client) Shutdown(ctx context.Context) error {
	c.closed.Store(true)
	c.bgCancel() // stop health check and any other background goroutines

	// Close priority queue to prevent new enqueues
	if c.priorityQueue != nil {
		c.priorityQueue.Close()
	}

	done := make(chan struct{})
	go func() {
		c.inFlight.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.CloseIdleConnections()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsHealthy reports whether the circuit breaker is in the Closed or Half-Open
// state, meaning the client will attempt to send requests. Returns true when no
// circuit breaker is configured.
func (c *Client) IsHealthy() bool {
	return c.CircuitBreakerState() != StateOpen
}

// Get returns a GET request builder for the given URL.
func (c *Client) Get(url string) *Request { return newRequest(http.MethodGet, url) }

// Post returns a POST request builder for the given URL.
func (c *Client) Post(url string) *Request { return newRequest(http.MethodPost, url) }

// Put returns a PUT request builder for the given URL.
func (c *Client) Put(url string) *Request { return newRequest(http.MethodPut, url) }

// Patch returns a PATCH request builder for the given URL.
func (c *Client) Patch(url string) *Request { return newRequest(http.MethodPatch, url) }

// Delete returns a DELETE request builder for the given URL.
func (c *Client) Delete(url string) *Request { return newRequest(http.MethodDelete, url) }

// Head returns a HEAD request builder for the given URL.
func (c *Client) Head(url string) *Request { return newRequest(http.MethodHead, url) }

// Options returns an OPTIONS request builder for the given URL.
func (c *Client) Options(url string) *Request { return newRequest(http.MethodOptions, url) }

// CircuitBreakerState returns the current state of the circuit breaker.
// Returns [StateClosed] when no circuit breaker is configured.
func (c *Client) CircuitBreakerState() CircuitBreakerState {
	if c.circuitBreaker == nil {
		return StateClosed
	}
	return c.circuitBreaker.State()
}

// ResetCircuitBreaker forces the circuit breaker back to the Closed state and
// clears all failure counters. Useful after a manual health check confirms
// downstream recovery.
func (c *Client) ResetCircuitBreaker() {
	if c.circuitBreaker != nil {
		c.circuitBreaker.Reset()
	}
}

// CloseIdleConnections closes any idle connections currently held in the
// transport's connection pool without interrupting active requests.
func (c *Client) CloseIdleConnections() { c.httpClient.CloseIdleConnections() }
