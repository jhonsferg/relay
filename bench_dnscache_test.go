package relay

import (
	"context"
	"net"
	"testing"
	"time"
)

// BenchmarkDNSCache_WarmDialPath measures the per-dial allocation cost when
// the DNS cache is warm (addresses already resolved). This isolates the hot
// path: cache lookup → pre-joined address lookup → base.DialContext.
//
// A pre-cancelled context makes base.DialContext return immediately
// (context.Canceled), keeping the benchmark deterministic and fast.
func BenchmarkDNSCache_WarmDialPath(b *testing.B) {
	cache := newDNSCache(time.Hour)
	cache.mu.Lock()
	cache.entries["example.com:80"] = dnsCacheEntry{
		addresses: []string{"127.0.0.1:80"},
		expiresAt: time.Now().Add(time.Hour),
	}
	cache.mu.Unlock()

	dialer := &cachedDialer{
		base:  &net.Dialer{},
		cache: cache,
	}

	bgCtx := context.Background()
	cancelledCtx, cancel := context.WithCancel(bgCtx)
	cancel() // pre-cancel so base.DialContext returns immediately

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = dialer.DialContext(cancelledCtx, "tcp", "example.com:80")
	}
}
