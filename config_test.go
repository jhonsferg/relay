package relay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

var testMu sync.Mutex

func TestWithConnectionPool(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	c := New(WithConnectionPool(50, 20, 100))
	if c.config.MaxIdleConns != 50 {
		t.Errorf("expected MaxIdleConns=50, got %d", c.config.MaxIdleConns)
	}
	if c.config.MaxIdleConnsPerHost != 20 {
		t.Errorf("expected MaxIdleConnsPerHost=20, got %d", c.config.MaxIdleConnsPerHost)
	}
	if c.config.MaxConnsPerHost != 100 {
		t.Errorf("expected MaxConnsPerHost=100, got %d", c.config.MaxConnsPerHost)
	}
}

func TestWithIdleConnTimeout(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	c := New(WithIdleConnTimeout(30 * time.Second))
	if c.config.IdleConnTimeout != 30*time.Second {
		t.Errorf("expected 30s, got %v", c.config.IdleConnTimeout)
	}
}

func TestWithResponseHeaderTimeout(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	c := New(WithResponseHeaderTimeout(5 * time.Second))
	if c.config.ResponseHeaderTimeout != 5*time.Second {
		t.Errorf("expected 5s, got %v", c.config.ResponseHeaderTimeout)
	}
}

func TestWithDialTimeout(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	c := New(WithDialTimeout(3 * time.Second))
	if c.config.DialTimeout != 3*time.Second {
		t.Errorf("expected 3s, got %v", c.config.DialTimeout)
	}
}

func TestWithDialKeepAlive(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	c := New(WithDialKeepAlive(60 * time.Second))
	if c.config.DialKeepAlive != 60*time.Second {
		t.Errorf("expected 60s, got %v", c.config.DialKeepAlive)
	}
}

func TestWithProxy(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	c := New(WithProxy("http://proxy.example.com:8080"))
	if c.config.ProxyURL != "http://proxy.example.com:8080" {
		t.Errorf("expected proxy URL to be set, got %q", c.config.ProxyURL)
	}
}

func TestWithCookieJar(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	jar, _ := cookiejar.New(nil)
	c := New(WithCookieJar(jar))
	if c.config.CookieJar != jar {
		t.Error("expected cookie jar to be set")
	}
}

func TestWithDefaultCookieJar(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	c := New(WithDefaultCookieJar())
	if c.config.CookieJar == nil {
		t.Error("expected default cookie jar to be set")
	}
}

func TestWithRetryIf(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	called := false
	retryFn := func(resp *http.Response, err error) bool {
		called = true
		return false // suppress the retry
	}
	srv := testutil.NewMockServer()
	defer srv.Close()
	// Enqueue a 500 so the built-in retry logic would normally retry.
	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})

	c := New(WithRetryIf(retryFn), WithDisableCircuitBreaker())
	c.Execute(c.Get(srv.URL() + "/")) //nolint:errcheck
	if !called {
		t.Error("expected RetryIf to be called")
	}
}

func TestWithOnRetry(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()

	var (
		mu        sync.Mutex
		callCount int
	)

	srv := testutil.NewMockServer()
	defer srv.Close()
	// Return 500 to trigger retries.
	for i := 0; i < 4; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	}

	c := New(
		WithDisableCircuitBreaker(),
		WithRetry(&RetryConfig{
			MaxAttempts:     3,
			InitialInterval: 0,
			MaxInterval:     0,
			RetryableStatus: []int{http.StatusInternalServerError},
			OnRetry: func(attempt int, resp *http.Response, err error) {
				mu.Lock()
				callCount++
				mu.Unlock()
			},
		}),
	)
	_, _ = c.Execute(c.Get(srv.URL() + "/"))

	mu.Lock()
	defer mu.Unlock()
	if callCount == 0 {
		t.Error("expected OnRetry to be called at least once")
	}
}

func TestWithOnStateChange(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()

	stateCh := make(chan CircuitBreakerState, 10)
	cfg := &CircuitBreakerConfig{
		MaxFailures:  1,
		ResetTimeout: time.Hour,
		OnStateChange: func(from, to CircuitBreakerState) {
			stateCh <- to
		},
	}

	cb := newCircuitBreaker(cfg)

	// Record failure directly to avoid any latency.
	cb.RecordFailure()

	if cb.State() != StateOpen {
		t.Errorf("expected Open state, got %s", cb.State())
	}

	select {
	case state := <-stateCh:
		if state != StateOpen {
			t.Errorf("expected Open state from callback, got %s", state)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("OnStateChange callback not triggered within 20s")
	}
}

func TestWithRateLimit(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	c := New(WithRateLimit(10, 1))
	if c.rateLimiter == nil {
		t.Error("expected rate limiter to be set")
	}
}

func TestWithDisableCompression(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	c := New(WithDisableCompression())
	if !c.config.DisableCompression {
		t.Error("expected DisableCompression=true")
	}
}

func TestWithMaxRedirects(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	c := New(WithMaxRedirects(5))
	if c.config.MaxRedirects != 5 {
		t.Errorf("expected MaxRedirects=5, got %d", c.config.MaxRedirects)
	}
}

func TestWithDisableRedirectTracking(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	c := New(WithDisableRedirectTracking())
	if !c.config.DisableRedirectTracking {
		t.Error("expected DisableRedirectTracking=true")
	}
}

func TestFinalizeConfig_AdaptiveTimeoutForcesTiming(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()

	cfg := AdaptiveTimeoutConfig{
		Percentile:     0.95,
		Multiplier:     2.0,
		WindowSize:     100,
		MinTimeout:     100 * time.Millisecond,
		MaxTimeout:     30 * time.Second,
		InitialTimeout: 5 * time.Second,
	}

	// Regardless of option order, adaptive timeout must force TimingEnabled.
	c1 := New(WithAdaptiveTimeout(cfg), WithDisableTiming())
	if !c1.config.TimingEnabled {
		t.Error("expected TimingEnabled=true when WithAdaptiveTimeout is set (option order: adaptive then disable)")
	}

	c2 := New(WithDisableTiming(), WithAdaptiveTimeout(cfg))
	if !c2.config.TimingEnabled {
		t.Error("expected TimingEnabled=true when WithAdaptiveTimeout is set (option order: disable then adaptive)")
	}

	c3 := New()
	if c3.config.TimingEnabled {
		t.Error("expected TimingEnabled=false by default with no adaptive timeout")
	}
}

// fakeExtension is a minimal Extension used to test WithExtension wiring.
type fakeExtension struct {
	name    string
	applyFn func(cfg *Config) error
}

func (f *fakeExtension) Name() string { return f.name }
func (f *fakeExtension) Apply(cfg *Config) error {
	return f.applyFn(cfg)
}

func TestWithExtension_ApplyRuns(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()

	var applied bool
	ext := &fakeExtension{
		name: "test-ext",
		applyFn: func(cfg *Config) error {
			applied = true
			cfg.TransportMiddlewares = append(cfg.TransportMiddlewares, func(rt http.RoundTripper) http.RoundTripper {
				return rt
			})
			return nil
		},
	}

	c := New(WithExtension(ext))
	if !applied {
		t.Error("expected Extension.Apply to be called during construction")
	}
	if len(c.config.TransportMiddlewares) != 1 {
		t.Errorf("expected extension's middleware to be registered, got %d middlewares", len(c.config.TransportMiddlewares))
	}
}

func TestWithExtension_ErrorIsLogged(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()

	logger := &configTestLogger{}
	ext := &fakeExtension{
		name: "broken-ext",
		applyFn: func(_ *Config) error {
			return errors.New("boom")
		},
	}

	// Construction must not fail/panic even though the extension errors.
	c := New(WithLogger(logger), WithExtension(ext))
	if c == nil {
		t.Fatal("expected New to return a client despite the extension error")
	}

	if !logger.hasWarnContaining("broken-ext") {
		t.Errorf("expected a Warn log mentioning the failing extension name, got: %+v", logger.entries)
	}
}

// TestWithExtension_NotReappliedOnWith guards against a bug where
// (*Client).With re-runs Extension.Apply for every extension the parent
// already applied. With clones c.config (config.go's clone() copies both
// cfg.extensions and the already-mutated cfg.TransportMiddlewares it
// produced), then buildClient's `for _, ext := range cfg.extensions { ...
// ext.Apply(cfg) ... }` loop applies every inherited extension a second
// time on top of a config that already reflects its first application. An
// Extension.Apply that appends middleware/hooks (the pattern the Extension
// interface's own doc comment and TestWithExtension_ApplyRuns describe) ends
// up registered twice after a single With call, and N+1 times after N
// chained With calls, for effects the extension author only asked to add
// once per client.
func TestWithExtension_NotReappliedOnWith(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()

	var applyCount int
	ext := &fakeExtension{
		name: "counting-ext",
		applyFn: func(cfg *Config) error {
			applyCount++
			cfg.TransportMiddlewares = append(cfg.TransportMiddlewares, func(rt http.RoundTripper) http.RoundTripper {
				return rt
			})
			return nil
		},
	}

	parent := New(WithExtension(ext))
	if applyCount != 1 {
		t.Fatalf("after New: applyCount = %d, want 1", applyCount)
	}
	if len(parent.config.TransportMiddlewares) != 1 {
		t.Fatalf("after New: %d middlewares registered, want 1", len(parent.config.TransportMiddlewares))
	}

	child := parent.With(WithTimeout(5 * time.Second))
	if applyCount != 1 {
		t.Errorf("after one With call: applyCount = %d, want 1 (extension re-applied on inherited config)", applyCount)
	}
	if len(child.config.TransportMiddlewares) != 1 {
		t.Errorf("after one With call: %d middlewares registered, want 1 (extension's middleware duplicated)", len(child.config.TransportMiddlewares))
	}

	grandchild := child.With(WithTimeout(10 * time.Second))
	if applyCount != 1 {
		t.Errorf("after a second chained With call: applyCount = %d, want 1", applyCount)
	}
	if len(grandchild.config.TransportMiddlewares) != 1 {
		t.Errorf("after a second chained With call: %d middlewares registered, want 1", len(grandchild.config.TransportMiddlewares))
	}

	var applyCount2 int
	ext2 := &fakeExtension{
		name: "second-ext",
		applyFn: func(cfg *Config) error {
			applyCount2++
			cfg.TransportMiddlewares = append(cfg.TransportMiddlewares, func(rt http.RoundTripper) http.RoundTripper {
				return rt
			})
			return nil
		},
	}
	withNewExt := grandchild.With(WithExtension(ext2))
	if applyCount != 1 {
		t.Errorf("after With(WithExtension(new)): first extension's applyCount = %d, want 1 (still shouldn't re-run)", applyCount)
	}
	if applyCount2 != 1 {
		t.Errorf("after With(WithExtension(new)): second extension's applyCount = %d, want 1", applyCount2)
	}
	if len(withNewExt.config.TransportMiddlewares) != 2 {
		t.Errorf("after With(WithExtension(new)): %d middlewares registered, want 2 (one per extension)", len(withNewExt.config.TransportMiddlewares))
	}
}

// configTestLogger captures Warn calls for TestWithExtension_ErrorIsLogged.
type configTestLogger struct {
	entries []string
}

func (l *configTestLogger) Debug(msg string, _ ...any) {}
func (l *configTestLogger) Info(msg string, _ ...any)  {}
func (l *configTestLogger) Warn(msg string, args ...any) {
	l.entries = append(l.entries, fmt.Sprintf("%s %v", msg, args))
}
func (l *configTestLogger) Error(msg string, _ ...any) {}

func (l *configTestLogger) hasWarnContaining(substr string) bool {
	for _, e := range l.entries {
		if strings.Contains(e, substr) {
			return true
		}
	}
	return false
}

func TestWithMaxResponseBodyBytes(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	c := New(WithMaxResponseBodyBytes(1024))
	if c.config.MaxResponseBodyBytes != 1024 {
		t.Errorf("expected MaxResponseBodyBytes=1024, got %d", c.config.MaxResponseBodyBytes)
	}
}

func TestWithTransportMiddleware(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	called := false
	mw := func(next http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			return next.RoundTrip(req)
		})
	}
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithTransportMiddleware(mw), WithDisableRetry(), WithDisableCircuitBreaker())
	c.Execute(c.Get(srv.URL() + "/")) //nolint:errcheck
	if !called {
		t.Error("expected transport middleware to be called")
	}
}

func TestWithOnBeforeRequest(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	called := false
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(
		WithOnBeforeRequest(func(ctx context.Context, req *Request) error {
			called = true
			return nil
		}),
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
	)
	c.Execute(c.Get(srv.URL() + "/")) //nolint:errcheck
	if !called {
		t.Error("expected OnBeforeRequest to be called")
	}
}

func TestWithOnAfterResponse(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	called := false
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(
		WithOnAfterResponse(func(ctx context.Context, resp *Response) error {
			called = true
			return nil
		}),
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
	)
	c.Execute(c.Get(srv.URL() + "/")) //nolint:errcheck
	if !called {
		t.Error("expected OnAfterResponse to be called")
	}
}

func TestWithDNSOverride(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	c := New(WithDNSOverride(map[string]string{"api.internal": "10.0.0.1"}))
	if c.config.DNSOverrides["api.internal"] != "10.0.0.1" {
		t.Errorf("expected DNS override to be set, got %q", c.config.DNSOverrides["api.internal"])
	}
}

// TestWithDNSOverride_RedirectsDial exercises overrideDialer.DialContext
// end-to-end (not just that the option populates Config.DNSOverrides): a
// nonexistent hostname is overridden to the mock server's real loopback IP,
// so a successful request proves the override actually rewrote the dial
// target rather than falling through to real DNS resolution (which would
// fail for a made-up hostname).
func TestWithDNSOverride_RedirectsDial(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "overridden"})

	u, err := url.Parse(srv.URL())
	if err != nil {
		t.Fatalf("parse mock server URL: %v", err)
	}
	_, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}

	const fakeHost = "relay-dns-override-test.invalid"
	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithDNSOverride(map[string]string{fakeHost: "127.0.0.1"}),
	)

	resp, err := c.Execute(c.Get("http://" + fakeHost + ":" + port + "/"))
	if err != nil {
		t.Fatalf("Execute (override should have redirected the dial to 127.0.0.1): %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestWithInMemoryCache(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	c := New(WithInMemoryCache(100))
	if c.config.CacheStore == nil {
		t.Error("expected cache store to be set")
	}
}

// TestWithURLNormalisation verifies URL normalisation mode configuration.
func TestWithURLNormalisation(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()

	tests := []struct {
		name     string
		mode     URLNormalisationMode
		expected URLNormalisationMode
	}{
		{"NormalisationAuto", NormalisationAuto, NormalisationAuto},
		{"NormalisationRFC3986", NormalisationRFC3986, NormalisationRFC3986},
		{"NormalisationAPI", NormalisationAPI, NormalisationAPI},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New(WithURLNormalisation(tt.mode))
			if c.config.URLNormalisationMode != tt.expected {
				t.Errorf("expected mode %v, got %v", tt.expected, c.config.URLNormalisationMode)
			}
		})
	}
}

// TestURLNormalisationMode_String verifies string representation of modes.
func TestURLNormalisationMode_String(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()

	tests := []struct {
		mode     URLNormalisationMode
		expected string
	}{
		{NormalisationAuto, "Auto"},
		{NormalisationRFC3986, "RFC3986"},
		{NormalisationAPI, "API"},
	}

	for _, tt := range tests {
		result := tt.mode.String()
		if result != tt.expected {
			t.Errorf("mode.String() expected %q, got %q", tt.expected, result)
		}
	}
}

// roundTripperFunc is a helper to create ad-hoc RoundTrippers in tests.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// TestIsAPIBase verifies API pattern detection for smart URL normalisation.
func TestIsAPIBase(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()

	tests := []struct {
		name     string
		baseURL  string
		expected bool
	}{
		// Empty/host-only URLs (should use RFC 3986 path)
		{"empty", "", false},
		{"host only", "http://api.example.com", false},
		{"host only with http", "https://api.example.com", false},
		{"host with trailing slash", "http://api.example.com/", false},

		// Common API patterns (should use safe string normalisation)
		{"odata path", "http://api.example.com/odata", true},
		{"api path", "http://api.example.com/api", true},
		{"v1 path", "http://api.example.com/v1", true},
		{"v2 path", "http://api.example.com/v2", true},
		{"v3 path", "http://api.example.com/v3", true},
		{"v4 path", "http://api.example.com/v4", true},
		{"v5 path", "http://api.example.com/v5", true},
		{"rest path", "http://api.example.com/rest", true},
		{"graphql path", "http://api.example.com/graphql", true},
		{"soap path", "http://api.example.com/soap", true},
		{"sap path", "http://api.example.com/sap", true},
		{"data path", "http://api.example.com/data", true},
		{"service path", "http://api.example.com/service", true},
		{"services path", "http://api.example.com/services", true},

		// Multi-segment paths (2+ slashes indicate API structure)
		{"multi-segment", "http://api.example.com/service/v1", true},
		{"multi-segment odata", "http://api.example.com/company/odata", true},
		{"deep path", "http://api.example.com/api/v1/data", true},

		// Invalid/malformed URLs (should handle gracefully)
		{"malformed", "not a url at all", false},
		{"invalid scheme", "ht!tp://api.example.com/v1", false},

		// Trailing slash variations
		{"v1 with trailing slash", "http://api.example.com/v1/", true},
		{"odata with trailing slash", "http://api.example.com/odata/", true},
		{"multi-segment with trailing slash", "http://api.example.com/api/v1/", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isAPIBase(tt.baseURL)
			if result != tt.expected {
				t.Errorf("isAPIBase(%q) = %v, want %v", tt.baseURL, result, tt.expected)
			}
		})
	}
}

// Phase 3: Auto-Normalisation Tests

func TestNormaliseBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Empty input
		{"empty string", "", ""},

		// Already has trailing slash
		{"host with slash", "http://api.com/", "http://api.com/"},
		{"api path with slash", "http://api.com/v1/", "http://api.com/v1/"},
		{"deep path with slash", "http://api.com/api/v1/data/", "http://api.com/api/v1/data/"},

		// Missing trailing slash (should add)
		{"host only", "http://api.com", "http://api.com/"},
		{"api path", "http://api.com/v1", "http://api.com/v1/"},
		{"deep path", "http://api.com/api/v1/data", "http://api.com/api/v1/data/"},

		// Various schemes
		{"https host", "https://api.com", "https://api.com/"},
		{"https with path", "https://api.com/v1", "https://api.com/v1/"},

		// Edge cases
		{"single slash", "/", "/"},
		{"path only", "/api", "/api/"},
		{"relative path", "api", "api/"},
		{"localhost", "http://localhost:8080", "http://localhost:8080/"},
		{"localhost with path", "http://localhost:8080/v1", "http://localhost:8080/v1/"},
		{"with query", "http://api.com?key=value", "http://api.com?key=value/"},
		{"with fragment", "http://api.com#section", "http://api.com#section/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormaliseBaseURL(tt.input)
			if result != tt.expected {
				t.Errorf("NormaliseBaseURL(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestWithAutoNormaliseURL(t *testing.T) {
	tests := []struct {
		name           string
		enable         bool
		urlStr         string
		expectedURL    string
		expectedParsed bool
	}{
		{
			name:           "auto normalise enabled",
			enable:         true,
			urlStr:         "http://api.com/v1",
			expectedURL:    "http://api.com/v1/",
			expectedParsed: true,
		},
		{
			name:           "auto normalise disabled",
			enable:         false,
			urlStr:         "http://api.com/v1",
			expectedURL:    "http://api.com/v1",
			expectedParsed: true,
		},
		{
			name:           "auto normalise enabled, already has slash",
			enable:         true,
			urlStr:         "http://api.com/v1/",
			expectedURL:    "http://api.com/v1/",
			expectedParsed: true,
		},
		{
			name:           "auto normalise disabled, no slash",
			enable:         false,
			urlStr:         "http://api.com/v1",
			expectedURL:    "http://api.com/v1",
			expectedParsed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				AutoNormaliseBaseURL: tt.enable,
			}
			option := WithAutoNormaliseURL(tt.enable)
			option(cfg)

			if cfg.AutoNormaliseBaseURL != tt.enable {
				t.Errorf("WithAutoNormaliseURL(%v) set AutoNormaliseBaseURL = %v, want %v",
					tt.enable, cfg.AutoNormaliseBaseURL, tt.enable)
			}
		})
	}
}

func TestWithBaseURL_AutoNormalise(t *testing.T) {
	tests := []struct {
		name          string
		autoNormalise bool
		input         string
		expectedURL   string
		shouldParsed  bool
	}{
		{
			name:          "auto normalise on, missing slash",
			autoNormalise: true,
			input:         "http://api.com/v1",
			expectedURL:   "http://api.com/v1/",
			shouldParsed:  true,
		},
		{
			name:          "auto normalise on, has slash",
			autoNormalise: true,
			input:         "http://api.com/v1/",
			expectedURL:   "http://api.com/v1/",
			shouldParsed:  true,
		},
		{
			name:          "auto normalise off, missing slash",
			autoNormalise: false,
			input:         "http://api.com/v1",
			expectedURL:   "http://api.com/v1",
			shouldParsed:  true,
		},
		{
			name:          "auto normalise off, has slash",
			autoNormalise: false,
			input:         "http://api.com/v1/",
			expectedURL:   "http://api.com/v1/",
			shouldParsed:  true,
		},
		{
			name:          "empty URL",
			autoNormalise: true,
			input:         "",
			expectedURL:   "",
			shouldParsed:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				AutoNormaliseBaseURL: tt.autoNormalise,
			}
			option := WithBaseURL(tt.input)
			option(cfg)

			if cfg.BaseURL != tt.expectedURL {
				t.Errorf("WithBaseURL(%q) with AutoNormalise=%v set BaseURL = %q, want %q",
					tt.input, tt.autoNormalise, cfg.BaseURL, tt.expectedURL)
			}

			if tt.shouldParsed {
				if cfg.parsedBaseURL == nil {
					t.Errorf("WithBaseURL(%q) did not parse URL, got nil", tt.input)
				}
			} else {
				if cfg.parsedBaseURL != nil {
					t.Errorf("WithBaseURL(%q) should not parse empty URL, got %v", tt.input, cfg.parsedBaseURL)
				}
			}
		})
	}
}

func TestAutoNormaliseURL_Default(t *testing.T) {
	cfg := defaultConfig()
	if !cfg.AutoNormaliseBaseURL {
		t.Errorf("defaultConfig().AutoNormaliseBaseURL = false, want true")
	}
}
