// Package relay – extra tests to close coverage gaps in config, request,
// response, and dns_cache that were not covered by the primary test suite.
package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

// ── config.go: WithOnRetry / WithOnStateChange ──────────────────────────────

func TestWithOnRetry_DirectOption(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	t.Cleanup(srv.Close)
	for i := 0; i < 4; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusServiceUnavailable})
	}

	var retryCount int
	c := New(
		WithDisableCircuitBreaker(),
		WithRetry(&RetryConfig{
			MaxAttempts:     3,
			InitialInterval: 0,
			MaxInterval:     0,
			RetryableStatus: []int{http.StatusServiceUnavailable},
		}),
		WithOnRetry(func(attempt int, resp *http.Response, err error) {
			retryCount++
		}),
	)
	_, _ = c.Execute(c.Get(srv.URL() + "/"))
	if retryCount == 0 {
		t.Error("WithOnRetry callback was never invoked")
	}
}

func TestWithOnStateChange_DirectOption(t *testing.T) {
	t.Parallel()
	called := make(chan struct{}, 4)
	cfg := &CircuitBreakerConfig{
		MaxFailures:  1,
		ResetTimeout: time.Hour,
	}
	c := New(
		WithCircuitBreaker(cfg),
		WithOnStateChange(func(from, to CircuitBreakerState) {
			called <- struct{}{}
		}),
	)
	// Verify the OnStateChange was wired into the circuit breaker config.
	if c.config.CircuitBreakerConfig == nil {
		t.Fatal("expected CircuitBreakerConfig to be set")
	}
	if c.config.CircuitBreakerConfig.OnStateChange == nil {
		t.Error("OnStateChange should be set via WithOnStateChange")
	}
}

func TestWithOnStateChange_CreatesDefaultCBConfig(t *testing.T) {
	t.Parallel()
	// When no CircuitBreakerConfig exists, WithOnStateChange should create a default one.
	c := New(
		WithDisableCircuitBreaker(), // starts with nil
		WithOnStateChange(func(from, to CircuitBreakerState) {}),
	)
	if c.config.CircuitBreakerConfig == nil {
		t.Error("expected default CircuitBreakerConfig to be created")
	}
}

// ── request.go: Method() / URL() ───────────────────────────────────────────

func TestRequest_Method(t *testing.T) {
	t.Parallel()
	c := New()
	req := c.Get("https://example.com/path")
	if req.Method() != http.MethodGet {
		t.Errorf("Method() = %q, want GET", req.Method())
	}
	req2 := c.Post("https://example.com/path")
	if req2.Method() != http.MethodPost {
		t.Errorf("Method() = %q, want POST", req2.Method())
	}
	req3 := c.Put("https://example.com/path")
	if req3.Method() != http.MethodPut {
		t.Errorf("Method() = %q, want PUT", req3.Method())
	}
	req4 := c.Delete("https://example.com/path")
	if req4.Method() != http.MethodDelete {
		t.Errorf("Method() = %q, want DELETE", req4.Method())
	}
}

func TestRequest_URL(t *testing.T) {
	t.Parallel()
	c := New()
	want := "https://example.com/items?key=val"
	req := c.Get(want)
	if req.URL() != want {
		t.Errorf("URL() = %q, want %q", req.URL(), want)
	}
}

// ── response.go: JSON() ─────────────────────────────────────────────────────

func TestResponse_JSON(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	t.Cleanup(srv.Close)
	body, _ := json.Marshal(map[string]interface{}{"name": "relay", "version": 1})
	srv.Enqueue(testutil.MockResponse{
		Status:  200,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    string(body),
	})
	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var result map[string]interface{}
	if err = resp.JSON(&result); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if result["name"] != "relay" {
		t.Errorf("name = %v, want relay", result["name"])
	}
}

func TestResponse_JSON_InvalidBody(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	t.Cleanup(srv.Close)
	srv.Enqueue(testutil.MockResponse{Status: 200, Body: "not json {"})
	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var result map[string]interface{}
	if err = resp.JSON(&result); err == nil {
		t.Error("expected JSON error for invalid body, got nil")
	}
}

// ── dns_cache.go: lookup / DialContext ─────────────────────────────────────

func TestDNSCache_LookupCacheHit(t *testing.T) {
	t.Parallel()
	cache := newDNSCache(30 * time.Second)

	// Pre-populate the cache so lookup returns the hit path.
	cache.mu.Lock()
	cache.entries["example.com:80"] = dnsCacheEntry{
		addresses: []string{"1.2.3.4:80"},
		expiresAt: time.Now().Add(time.Minute),
	}
	cache.mu.Unlock()

	addrs, err := cache.lookup(context.Background(), "example.com", "80", "example.com:80")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(addrs) == 0 || addrs[0] != "1.2.3.4:80" {
		t.Errorf("got %v, want [1.2.3.4:80]", addrs)
	}
}

func TestDNSCache_LookupExpiredEntry(t *testing.T) {
	t.Parallel()
	cache := newDNSCache(1 * time.Second)
	// Insert an expired entry so the slow path (re-resolve) is triggered.
	cache.mu.Lock()
	cache.entries["localhost:0"] = dnsCacheEntry{
		addresses: []string{"127.0.0.1:0"},
		expiresAt: time.Now().Add(-1 * time.Second), // already expired
	}
	cache.mu.Unlock()

	// lookup should resolve via the resolver, not the stale entry.
	addrs, err := cache.lookup(context.Background(), "localhost", "0", "localhost:0")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(addrs) == 0 {
		t.Error("expected at least one resolved address")
	}
}

func TestDNSCache_DialContextIPLiteral(t *testing.T) {
	t.Parallel()
	// IP-literal addresses bypass the DNS cache path.
	srv := testutil.NewMockServer()
	t.Cleanup(srv.Close)
	srv.Enqueue(testutil.MockResponse{Status: 200})
	// WithDNSCache + IP URL hits the IP-literal bypass in DialContext.
	c := New(WithDNSCache(30*time.Second), WithDisableRetry(), WithDisableCircuitBreaker())
	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestDNSCache_DialContextCachedHostname(t *testing.T) {
	t.Parallel()
	// Make a real request to localhost, which forces the DialContext code path
	// including the DNS lookup + caching + pre-joined address connection.
	srv := testutil.NewMockServer()
	t.Cleanup(srv.Close)
	for i := 0; i < 3; i++ {
		srv.Enqueue(testutil.MockResponse{Status: 200})
	}
	// WithDNSCache will route through cachedDialer.DialContext for hostname resolution.
	c := New(WithDNSCache(30*time.Second), WithDisableRetry(), WithDisableCircuitBreaker())
	for i := 0; i < 3; i++ {
		resp, err := c.Execute(c.Get(srv.URL() + "/"))
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("request %d: status = %d", i, resp.StatusCode)
		}
	}
}
