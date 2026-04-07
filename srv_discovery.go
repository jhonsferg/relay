package relay

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SRVBalancer selects from a list of SRV targets.
type SRVBalancer int

const (
	SRVRoundRobin SRVBalancer = iota // rotate through targets in order
	SRVRandom                        // pick a random target each time
	SRVPriority                      // pick lowest-priority target (highest priority = lowest number)
)

// SRVOption configures an SRVResolver.
type SRVOption func(*SRVResolver)

// WithSRVTTL caches SRV lookup results for the given duration.
func WithSRVTTL(d time.Duration) SRVOption {
	return func(r *SRVResolver) { r.ttl = d }
}

// WithSRVBalancer sets the load balancing strategy.
func WithSRVBalancer(b SRVBalancer) SRVOption {
	return func(r *SRVResolver) { r.balancer = b }
}

// srvTarget holds a resolved SRV host and port.
type srvTarget struct {
	host string
	port uint16
}

// SRVResolver resolves DNS SRV records to select a backend host:port.
type SRVResolver struct {
	service  string // e.g. "http" or "https"
	proto    string // "tcp" or "udp"
	name     string // e.g. "myservice.example.com"
	scheme   string // "http" or "https"
	balancer SRVBalancer
	ttl      time.Duration // how long to cache resolved addresses; 0 = no cache

	mu       sync.Mutex
	cached   []srvTarget
	cacheExp time.Time
	rrIdx    atomic.Uint64

	// lookupSRV is the DNS SRV lookup function. Defaults to net.DefaultResolver.LookupSRV.
	// Override for testing.
	lookupSRV func(ctx context.Context, service, proto, name string) (string, []*net.SRV, error)
}

// NewSRVResolver creates a resolver for the given SRV record.
//   - service: the service name (e.g. "http", "https", "grpc")
//   - proto: the protocol ("tcp" or "udp")
//   - name: the domain to query (e.g. "_http._tcp.myservice.example.com" or just "myservice.example.com")
//   - scheme: "http" or "https" for building URLs
func NewSRVResolver(service, proto, name, scheme string, opts ...SRVOption) *SRVResolver {
	r := &SRVResolver{
		service:   service,
		proto:     proto,
		name:      name,
		scheme:    scheme,
		lookupSRV: net.DefaultResolver.LookupSRV,
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Resolve performs a DNS SRV lookup and returns the selected target as "host:port".
// Uses cached results if within TTL.
func (r *SRVResolver) Resolve(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if r.ttl > 0 && len(r.cached) > 0 && now.Before(r.cacheExp) {
		return r.pickTarget(r.cached), nil
	}

	_, addrs, err := r.lookupSRV(ctx, r.service, r.proto, r.name)
	if err != nil {
		return "", fmt.Errorf("srv lookup %s.%s.%s: %w", r.service, r.proto, r.name, err)
	}
	if len(addrs) == 0 {
		return "", fmt.Errorf("srv lookup %s.%s.%s: no records", r.service, r.proto, r.name)
	}

	if r.balancer == SRVPriority {
		slices.SortFunc(addrs, func(a, b *net.SRV) int {
			if a.Priority < b.Priority {
				return -1
			}
			if a.Priority > b.Priority {
				return 1
			}
			return 0
		})
	}

	targets := make([]srvTarget, len(addrs))
	for i, a := range addrs {
		targets[i] = srvTarget{
			host: strings.TrimSuffix(a.Target, "."),
			port: a.Port,
		}
	}

	if r.ttl > 0 {
		r.cached = targets
		r.cacheExp = now.Add(r.ttl)
	}

	return r.pickTarget(targets), nil
}

// pickTarget selects a target from the list using the configured balancer strategy.
// Must be called with mu held.
func (r *SRVResolver) pickTarget(targets []srvTarget) string {
	var t srvTarget
	switch r.balancer {
	case SRVRoundRobin:
		idx := r.rrIdx.Add(1) - 1
		// G115 is safe here: counter wraps via modulo, never exceeds len(targets)
		//nolint:gosec
		t = targets[int(idx)%len(targets)]
	case SRVRandom:
		// G404 is safe here: math/rand/v2 is used, not weak crypto/rand
		//nolint:gosec
		t = targets[rand.IntN(len(targets))]
	default: // SRVPriority — list is already sorted ascending by priority
		t = targets[0]
	}
	return fmt.Sprintf("%s:%d", t.host, t.port)
}

// srvRoundTripper is an http.RoundTripper that rewrites the request host
// using an SRVResolver before forwarding to the next transport.
type srvRoundTripper struct {
	next     http.RoundTripper
	resolver *SRVResolver
}

func (s srvRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	target, err := s.resolver.Resolve(req.Context())
	if err != nil {
		return nil, fmt.Errorf("srv resolve: %w", err)
	}
	reqCopy := req.Clone(req.Context())
	reqCopy.URL.Host = target
	reqCopy.Host = target
	return s.next.RoundTrip(reqCopy)
}

// RoundTripperMiddleware returns a relay-compatible middleware that rewrites
// the request host to the SRV-resolved address before each request.
func (r *SRVResolver) RoundTripperMiddleware() func(http.RoundTripper) http.RoundTripper {
	return func(next http.RoundTripper) http.RoundTripper {
		return srvRoundTripper{next: next, resolver: r}
	}
}

// WithSRVDiscovery sets an SRVResolver on the client. Before each request,
// the resolver is called and the request Host is replaced with the resolved target.
func WithSRVDiscovery(resolver *SRVResolver) Option {
	return WithTransportMiddleware(resolver.RoundTripperMiddleware())
}
