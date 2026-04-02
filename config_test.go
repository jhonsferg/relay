package relay

import (
	"context"
	"net/http"
	"net/http/cookiejar"
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

func TestWithInMemoryCache(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()
	c := New(WithInMemoryCache(100))
	if c.config.CacheStore == nil {
		t.Error("expected cache store to be set")
	}
}

// TestWithURLNormalization verifies URL normalization mode configuration.
func TestWithURLNormalization(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()

	tests := []struct {
		name     string
		mode     URLNormalizationMode
		expected URLNormalizationMode
	}{
		{"NormalizationAuto", NormalizationAuto, NormalizationAuto},
		{"NormalizationRFC3986", NormalizationRFC3986, NormalizationRFC3986},
		{"NormalizationAPI", NormalizationAPI, NormalizationAPI},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New(WithURLNormalization(tt.mode))
			if c.config.URLNormalizationMode != tt.expected {
				t.Errorf("expected mode %v, got %v", tt.expected, c.config.URLNormalizationMode)
			}
		})
	}
}

// TestURLNormalizationMode_String verifies string representation of modes.
func TestURLNormalizationMode_String(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()

	tests := []struct {
		mode     URLNormalizationMode
		expected string
	}{
		{NormalizationAuto, "Auto"},
		{NormalizationRFC3986, "RFC3986"},
		{NormalizationAPI, "API"},
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

// TestIsAPIBase verifies API pattern detection for smart URL normalization.
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

		// Common API patterns (should use safe string normalization)
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

// Phase 3: Auto-Normalization Tests

func TestNormalizeBaseURL(t *testing.T) {
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
			result := NormalizeBaseURL(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeBaseURL(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestWithAutoNormalizeURL(t *testing.T) {
	tests := []struct {
		name           string
		enable         bool
		urlStr         string
		expectedURL    string
		expectedParsed bool
	}{
		{
			name:           "auto normalize enabled",
			enable:         true,
			urlStr:         "http://api.com/v1",
			expectedURL:    "http://api.com/v1/",
			expectedParsed: true,
		},
		{
			name:           "auto normalize disabled",
			enable:         false,
			urlStr:         "http://api.com/v1",
			expectedURL:    "http://api.com/v1",
			expectedParsed: true,
		},
		{
			name:           "auto normalize enabled, already has slash",
			enable:         true,
			urlStr:         "http://api.com/v1/",
			expectedURL:    "http://api.com/v1/",
			expectedParsed: true,
		},
		{
			name:           "auto normalize disabled, no slash",
			enable:         false,
			urlStr:         "http://api.com/v1",
			expectedURL:    "http://api.com/v1",
			expectedParsed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				AutoNormalizeBaseURL: tt.enable,
			}
			option := WithAutoNormalizeURL(tt.enable)
			option(cfg)

			if cfg.AutoNormalizeBaseURL != tt.enable {
				t.Errorf("WithAutoNormalizeURL(%v) set AutoNormalizeBaseURL = %v, want %v",
					tt.enable, cfg.AutoNormalizeBaseURL, tt.enable)
			}
		})
	}
}

func TestWithBaseURL_AutoNormalize(t *testing.T) {
	tests := []struct {
		name           string
		autoNormalize  bool
		input          string
		expectedURL    string
		shouldParsed   bool
	}{
		{
			name:          "auto normalize on, missing slash",
			autoNormalize: true,
			input:         "http://api.com/v1",
			expectedURL:   "http://api.com/v1/",
			shouldParsed:  true,
		},
		{
			name:          "auto normalize on, has slash",
			autoNormalize: true,
			input:         "http://api.com/v1/",
			expectedURL:   "http://api.com/v1/",
			shouldParsed:  true,
		},
		{
			name:          "auto normalize off, missing slash",
			autoNormalize: false,
			input:         "http://api.com/v1",
			expectedURL:   "http://api.com/v1",
			shouldParsed:  true,
		},
		{
			name:          "auto normalize off, has slash",
			autoNormalize: false,
			input:         "http://api.com/v1/",
			expectedURL:   "http://api.com/v1/",
			shouldParsed:  true,
		},
		{
			name:          "empty URL",
			autoNormalize: true,
			input:         "",
			expectedURL:   "",
			shouldParsed:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				AutoNormalizeBaseURL: tt.autoNormalize,
			}
			option := WithBaseURL(tt.input)
			option(cfg)

			if cfg.BaseURL != tt.expectedURL {
				t.Errorf("WithBaseURL(%q) with AutoNormalize=%v set BaseURL = %q, want %q",
					tt.input, tt.autoNormalize, cfg.BaseURL, tt.expectedURL)
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

func TestAutoNormalizeURL_Default(t *testing.T) {
	cfg := defaultConfig()
	if !cfg.AutoNormalizeBaseURL {
		t.Errorf("defaultConfig().AutoNormalizeBaseURL = false, want true")
	}
}

