package relay

import (
	"context"
	"net"
	"sync"
	"time"
)

// DNSCacheConfig controls client-side DNS result caching.
type DNSCacheConfig struct {
	// TTL is how long a resolved address set is considered valid.
	// After expiry the next dial for that hostname triggers a fresh resolution.
	// A typical value is 30 s–5 min depending on how often your upstreams
	// rotate IPs and how quickly you need to detect changes.
	TTL time.Duration
}

// dnsCacheEntry is a single cached DNS result.
type dnsCacheEntry struct {
	addrs     []string  // resolved IP addresses
	expiresAt time.Time // wall-clock expiry
}

// dnsCache caches DNS lookups for a configurable TTL.
// It is safe for concurrent use.
type dnsCache struct {
	mu       sync.RWMutex
	entries  map[string]dnsCacheEntry
	ttl      time.Duration
	resolver *net.Resolver
}

func newDNSCache(ttl time.Duration) *dnsCache {
	return &dnsCache{
		entries:  make(map[string]dnsCacheEntry),
		ttl:      ttl,
		resolver: net.DefaultResolver,
	}
}

// lookup returns cached addresses for host or resolves them via the system
// resolver and caches the result.
func (c *dnsCache) lookup(ctx context.Context, host string) ([]string, error) {
	// Fast path: cache hit.
	c.mu.RLock()
	entry, ok := c.entries[host]
	c.mu.RUnlock()

	if ok && time.Now().Before(entry.expiresAt) {
		return entry.addrs, nil
	}

	// Slow path: resolve and cache.
	addrs, err := c.resolver.LookupHost(ctx, host)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.entries[host] = dnsCacheEntry{
		addrs:     addrs,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()

	return addrs, nil
}

// cachedDialer wraps a [net.Dialer] and uses a [dnsCache] to resolve hostnames
// before dialing, so that repeated dials to the same host skip the OS resolver.
type cachedDialer struct {
	base  *net.Dialer
	cache *dnsCache
}

// DialContext resolves host via the DNS cache, then dials the first successful
// address. Mirrors the standard dialer's Happy Eyeballs behaviour by trying
// all resolved addresses in order.
func (d *cachedDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return d.base.DialContext(ctx, network, addr)
	}

	// Skip cache for IP-literal addresses — no DNS needed.
	if net.ParseIP(host) != nil {
		return d.base.DialContext(ctx, network, addr)
	}

	addrs, err := d.cache.lookup(ctx, host)
	if err != nil {
		// Fall back to the base dialer on resolution failure.
		return d.base.DialContext(ctx, network, addr)
	}

	var lastErr error
	for _, ip := range addrs {
		resolved := net.JoinHostPort(ip, port)
		conn, dialErr := d.base.DialContext(ctx, network, resolved)
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
		if ctx.Err() != nil {
			break
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return d.base.DialContext(ctx, network, addr)
}
