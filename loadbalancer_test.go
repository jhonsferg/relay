package relay

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoadBalancer_RoundRobin(t *testing.T) {
	lb := newLoadBalancer(&LoadBalancerConfig{
		Backends: []string{"http://a", "http://b", "http://c"},
		Strategy: RoundRobin,
	})

	got := make([]string, 6)
	for i := range got {
		b, err := lb.selectBackend()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got[i] = b
	}

	// Round-robin should cycle: a, b, c, a, b, c
	want := []string{"http://a", "http://b", "http://c", "http://a", "http://b", "http://c"}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("request %d: got %q, want %q", i, got[i], w)
		}
	}
}

func TestLoadBalancer_Random(t *testing.T) {
	backends := []string{"http://a", "http://b", "http://c"}
	lb := newLoadBalancer(&LoadBalancerConfig{
		Backends: backends,
		Strategy: Random,
	})

	set := make(map[string]bool)
	for i := 0; i < 100; i++ {
		b, err := lb.selectBackend()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		set[b] = true
	}
	// With 100 requests across 3 backends, all should be selected at least once.
	for _, be := range backends {
		if !set[be] {
			t.Errorf("backend %q never selected in 100 requests", be)
		}
	}
}

func TestLoadBalancer_DefaultStrategyIsRoundRobin(t *testing.T) {
	lb := newLoadBalancer(&LoadBalancerConfig{
		Backends: []string{"http://a", "http://b"},
		// Strategy intentionally omitted - should default to RoundRobin
	})
	if lb.strategy != RoundRobin {
		t.Errorf("expected default strategy RoundRobin, got %q", lb.strategy)
	}
}

func TestLoadBalancer_SingleBackend(t *testing.T) {
	lb := newLoadBalancer(&LoadBalancerConfig{
		Backends: []string{"http://only"},
	})
	for i := 0; i < 5; i++ {
		b, err := lb.selectBackend()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if b != "http://only" {
			t.Errorf("got %q, want %q", b, "http://only")
		}
	}
}

func TestLoadBalancer_NilConfigReturnsNil(t *testing.T) {
	if lb := newLoadBalancer(nil); lb != nil {
		t.Error("expected nil loadBalancer from nil config")
	}
}

func TestLoadBalancer_EmptyBackendsReturnsNil(t *testing.T) {
	if lb := newLoadBalancer(&LoadBalancerConfig{Backends: nil}); lb != nil {
		t.Error("expected nil loadBalancer from empty backends")
	}
}

func TestLoadBalancer_SelectBackendOnNilReturnsError(t *testing.T) {
	var lb *loadBalancer
	_, err := lb.selectBackend()
	if err == nil {
		t.Error("expected error from nil loadBalancer.selectBackend()")
	}
}

func TestLoadBalancer_IntegrationRoundRobin(t *testing.T) {
	var hitA, hitB atomic.Int32

	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hitA.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srvA.Close()

	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hitB.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srvB.Close()

	c := New(
		WithDisableCircuitBreaker(),
		WithDisableRetry(),
		WithLoadBalancer(LoadBalancerConfig{
			Backends: []string{srvA.URL, srvB.URL},
			Strategy: RoundRobin,
		}),
	)

	for i := 0; i < 6; i++ {
		resp, err := c.Execute(c.Get("/ping"))
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		_ = resp
	}

	if hitA.Load() != 3 || hitB.Load() != 3 {
		t.Errorf("expected 3 hits each, got A=%d B=%d", hitA.Load(), hitB.Load())
	}
}

func TestLoadBalancer_ThreadSafety(t *testing.T) {
	var hitA, hitB atomic.Int32

	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hitA.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srvA.Close()

	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hitB.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srvB.Close()

	c := New(
		WithDisableCircuitBreaker(),
		WithDisableRetry(),
		WithTimeout(5*time.Second),
		WithLoadBalancer(LoadBalancerConfig{
			Backends: []string{srvA.URL, srvB.URL},
			Strategy: RoundRobin,
		}),
	)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			resp, err := c.Execute(c.Get("/ping"))
			if err != nil {
				t.Errorf("concurrent request failed: %v", err)
				return
			}
			_ = resp
		}()
	}
	wg.Wait()

	total := hitA.Load() + hitB.Load()
	if total != n {
		t.Errorf("expected %d total hits, got %d", n, total)
	}
}

func TestWithLoadBalancer_Option(t *testing.T) {
	cfg := defaultConfig()
	WithLoadBalancer(LoadBalancerConfig{
		Backends: []string{"http://x", "http://y"},
		Strategy: Random,
	})(cfg)

	if cfg.LoadBalancerConfig == nil {
		t.Fatal("expected LoadBalancerConfig to be set")
	}
	if len(cfg.LoadBalancerConfig.Backends) != 2 {
		t.Errorf("expected 2 backends, got %d", len(cfg.LoadBalancerConfig.Backends))
	}
	if cfg.LoadBalancerConfig.Strategy != Random {
		t.Errorf("expected Random strategy, got %q", cfg.LoadBalancerConfig.Strategy)
	}
}

func TestLoadBalancer_InvalidBackendURLReturnsError(t *testing.T) {
	c := New(
		WithDisableCircuitBreaker(),
		WithDisableRetry(),
		WithLoadBalancer(LoadBalancerConfig{
			Backends: []string{"://invalid-url"},
		}),
	)

	_, err := c.Execute(c.Get("/test"))
	if err == nil {
		t.Error("expected error for invalid backend URL")
	}
}

func TestLoadBalancer_PathCombination(t *testing.T) {
	called := make([]string, 0, 2)
	var mu sync.Mutex

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		called = append(called, fmt.Sprintf("%s%s", r.Host, r.URL.Path))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(
		WithDisableCircuitBreaker(),
		WithDisableRetry(),
		WithLoadBalancer(LoadBalancerConfig{
			Backends: []string{srv.URL},
		}),
	)

	_, err := c.Execute(c.Get("/api/users"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(called) == 0 {
		t.Error("expected at least one request")
	}
}
