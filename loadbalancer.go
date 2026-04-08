package relay

import (
	"fmt"
	"math/rand/v2"
	"net/url"
	"sync/atomic"
)

// LoadBalancerStrategy defines the algorithm used to select backends.
type LoadBalancerStrategy string

const (
	// RoundRobin distributes requests sequentially across backends.
	// This is the default strategy.
	RoundRobin LoadBalancerStrategy = "round-robin"

	// Random selects a backend uniformly at random on each request.
	Random LoadBalancerStrategy = "random"
)

// LoadBalancerConfig configures client-side load balancing across multiple backends.
type LoadBalancerConfig struct {
	// Backends is the list of base URLs to balance across.
	// Must not be empty when used.
	Backends []string

	// Strategy selects the load balancing algorithm.
	// Defaults to RoundRobin if empty.
	Strategy LoadBalancerStrategy
}

// loadBalancer encapsulates the internal state for load balancing.
type loadBalancer struct {
	backends          []string
	strategy          LoadBalancerStrategy
	roundRobinCounter atomic.Uint64
	initErr           error // set at construction if any backend is invalid
}

// newLoadBalancer creates a load balancer from a config.
// Returns nil if config is nil or backends are empty.
// Validation errors (empty, malformed or non-http(s) backend URLs) are stored
// internally and surfaced on the first selectBackend() call, so New() can
// remain error-free while still detecting misconfiguration early.
func newLoadBalancer(cfg *LoadBalancerConfig) *loadBalancer {
	if cfg == nil || len(cfg.Backends) == 0 {
		return nil
	}

	strategy := cfg.Strategy
	if strategy == "" {
		strategy = RoundRobin
	}

	lb := &loadBalancer{
		backends: cfg.Backends,
		strategy: strategy,
	}

	for i, backend := range cfg.Backends {
		if backend == "" {
			lb.initErr = fmt.Errorf("relay: load balancer backend[%d] is empty", i)
			return lb
		}
		u, err := url.ParseRequestURI(backend)
		if err != nil {
			lb.initErr = fmt.Errorf("relay: load balancer backend[%d] %q is not a valid URL: %w", i, backend, err)
			return lb
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			lb.initErr = fmt.Errorf("relay: load balancer backend[%d] %q must use http or https scheme", i, backend)
			return lb
		}
	}

	return lb
}

// selectBackend returns the backend URL for the next request.
// Thread-safe.
func (lb *loadBalancer) selectBackend() (string, error) {
	if lb == nil || len(lb.backends) == 0 {
		return "", fmt.Errorf("load balancer has no backends configured")
	}

	if lb.initErr != nil {
		return "", lb.initErr
	}
	var idx int
	lenBackends := len(lb.backends)
	switch lb.strategy {
	case RoundRobin:
		// G115 is safe here: counter will never exceed lenBackends due to modulo operation
		//
		//nolint:gosec
		idx = int(lb.roundRobinCounter.Add(1)-1) % lenBackends
	case Random:
		// G404 is safe here: math/rand/v2 is used, not weak crypto/rand
		//
		//nolint:gosec
		idx = rand.IntN(lenBackends)
	default:
		// G115 is safe here: counter will never exceed lenBackends due to modulo operation
		//
		//nolint:gosec
		idx = int(lb.roundRobinCounter.Add(1)-1) % lenBackends
	}

	return lb.backends[idx], nil
}
