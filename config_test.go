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
	callCount := 0
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
				callCount++
			},
		}),
	)
	c.Execute(c.Get(srv.URL() + "/")) //nolint:errcheck
	if callCount == 0 {
		t.Error("expected OnRetry to be called at least once")
	}
}

func TestWithOnStateChange(t *testing.T) {
	testMu.Lock()
	defer testMu.Unlock()

	srv := testutil.NewMockServer()
	defer srv.Close()

	stateCh := make(chan CircuitBreakerState, 100)
	onStateChange := func(from, to CircuitBreakerState) {
		select {
		case stateCh <- to:
		default:
		}
	}

	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithCircuitBreaker(&CircuitBreakerConfig{
			MaxFailures:  1, // Trip on first failure
			ResetTimeout: time.Hour,
		}),
		WithOnStateChange(onStateChange),
	)

	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	_, _ = c.Execute(c.Get(srv.URL() + "/fail"))

	// Terminal check loop
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if c.CircuitBreakerState() == StateOpen {
			return
		}
		select {
		case state := <-stateCh:
			if state == StateOpen {
				return
			}
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Fatal("OnStateChange (Open) not triggered within 15s")
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

// roundTripperFunc is a helper to create ad-hoc RoundTrippers in tests.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
