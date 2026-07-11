package relay

import (
	"context"
	"net"
	"sync"
	"time"
)

// call represents an in-flight or completed DNS resolution.
type call struct {
	addresses []string
	err       error
	done      chan struct{}
}

// flightGroup coalesces concurrent lookups for the same key so only one
// goroutine performs the resolution; all others share the result.
type flightGroup struct {
	mu    sync.Mutex
	calls map[string]*call
}

func (g *flightGroup) do(key string, fn func() ([]string, error)) ([]string, error) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = make(map[string]*call)
	}
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		<-c.done
		return c.addresses, c.err
	}
	c := &call{done: make(chan struct{})}
	g.calls[key] = c
	g.mu.Unlock()

	c.addresses, c.err = fn()
	close(c.done)

	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()
	return c.addresses, c.err
}

// DNSCacheConfig controls client-side DNS result caching.
type DNSCacheConfig struct {
	// TTL is how long a resolved address set is considered valid.
	// After expiry the next dial for that hostname triggers a fresh resolution.
	// A typical value is 30 s–5 min depending on how often your upstreams
	// rotate IPs and how quickly you need to detect changes.
	TTL time.Duration
}

// dnsCacheEntry is a single cached DNS result.
// addresses holds pre-joined "ip:port" strings so DialContext avoids calling
// net.JoinHostPort on every dial attempt (one allocation per address saved).
type dnsCacheEntry struct {
	addresses []string  // pre-joined "ip:port" strings
	expiresAt time.Time // wall-clock expiry
}

// dnsCache caches DNS lookups for a configurable TTL.
// It is safe for concurrent use.
type dnsCache struct {
	mu       sync.RWMutex
	entries  map[string]dnsCacheEntry
	ttl      time.Duration
	resolver *net.Resolver
	flights  flightGroup // coalesces concurrent cache-miss resolutions
}

func newDNSCache(ttl time.Duration) *dnsCache {
	return &dnsCache{
		entries:  make(map[string]dnsCacheEntry),
		ttl:      ttl,
		resolver: net.DefaultResolver,
	}
}

// lookup returns pre-joined "ip:port" addresses for the given host+port pair.
// On a cache hit the entry is returned without any string allocation.
// On a cache miss the host is resolved via the system resolver and the results
// are pre-joined with port and stored under cacheKey (the original "host:port"
// addr string), avoiding repeated net.JoinHostPort calls on subsequent dials.
func (c *dnsCache) lookup(ctx context.Context, host, port, cacheKey string) ([]string, error) {
	// Fast path: cache hit - return pre-joined addresses without allocation.
	c.mu.RLock()
	entry, ok := c.entries[cacheKey]
	c.mu.RUnlock()

	if ok && time.Now().Before(entry.expiresAt) {
		return entry.addresses, nil
	}

	// Slow path: use singleflight to prevent thundering herd when
	// multiple goroutines miss the cache simultaneously.
	addresses, err := c.flights.do(cacheKey, func() ([]string, error) {
		// Double-check cache after acquiring the flight slot.
		c.mu.RLock()
		entry, ok := c.entries[cacheKey]
		c.mu.RUnlock()
		if ok && time.Now().Before(entry.expiresAt) {
			return entry.addresses, nil
		}

		ips, lookupErr := c.resolver.LookupHost(ctx, host)
		if lookupErr != nil {
			return nil, lookupErr
		}

		addresses := make([]string, len(ips))
		for i, ip := range ips {
			addresses[i] = net.JoinHostPort(ip, port)
		}

		c.mu.Lock()
		c.entries[cacheKey] = dnsCacheEntry{
			addresses: addresses,
			expiresAt: time.Now().Add(c.ttl),
		}
		c.mu.Unlock()

		return addresses, nil
	})
	if err != nil {
		return nil, err
	}
	return addresses, nil
}

// cachedDialer wraps a [net.Dialer] and uses a [dnsCache] to resolve hostnames
// before dialling, so that repeated dials to the same host skip the OS resolver.
type cachedDialer struct {
	base  *net.Dialer
	cache *dnsCache
}

// DialContext resolves host via the DNS cache, then dials the first successful
// address. Mirrors the standard dialer's Happy Eyeballs behaviour by trying
// all resolved addresses in order.
//
// Addresses are stored pre-joined ("ip:port") in the cache so the hot path
// avoids calling net.JoinHostPort on every dial attempt.
func (d *cachedDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return d.base.DialContext(ctx, network, addr)
	}

	// Skip cache for IP-literal addresses - no DNS needed.
	if net.ParseIP(host) != nil {
		return d.base.DialContext(ctx, network, addr)
	}

	// addr is already "host:port" - use it directly as the cache key
	// to avoid an extra string allocation for the lookup.
	addresses, err := d.cache.lookup(ctx, host, port, addr)
	if err != nil {
		// Fall back to the base dialer on resolution failure.
		return d.base.DialContext(ctx, network, addr)
	}

	var lastErr error
	for _, resolved := range addresses {
		// resolved is pre-joined "ip:port" - no net.JoinHostPort needed.
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
