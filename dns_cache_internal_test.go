package relay

import (
	"context"
	"testing"
	"time"
)

// TestDNSCache_DetachesLeaderContext guards against a regression where a
// cache-miss resolution ran under the first caller's own context. If that
// caller's context was canceled while the coalesced (singleflight) lookup
// was still in flight, every other goroutine waiting on the same cache key
// would receive that same cancellation error too - even ones whose own
// context was perfectly healthy.
func TestDNSCache_DetachesLeaderContext(t *testing.T) {
	c := newDNSCache(time.Minute)

	ctx, cancel := context.WithCancel(context.Background())

	c.lookupHost = func(lookupCtx context.Context, _ string) ([]string, error) {
		// Simulate the triggering caller's own context expiring while the
		// shared resolution is still in flight.
		cancel()
		if err := lookupCtx.Err(); err != nil {
			return nil, err
		}
		return []string{"127.0.0.1"}, nil
	}

	addresses, err := c.lookup(ctx, "example.com", "443", "example.com:443")
	if err != nil {
		t.Fatalf("expected success despite the triggering context being canceled mid-flight, got: %v", err)
	}
	if len(addresses) != 1 || addresses[0] != "127.0.0.1:443" {
		t.Errorf("addresses = %v, want [127.0.0.1:443]", addresses)
	}
}

// TestDNSCache_FollowerSharesLeaderResult confirms the coalesced lookup path
// still works end-to-end (cache populated, single lookupHost call) alongside
// the context-detachment fix above.
func TestDNSCache_FollowerSharesLeaderResult(t *testing.T) {
	c := newDNSCache(time.Minute)

	var calls int
	c.lookupHost = func(context.Context, string) ([]string, error) {
		calls++
		return []string{"10.0.0.1"}, nil
	}

	addr1, err := c.lookup(context.Background(), "example.com", "80", "example.com:80")
	if err != nil {
		t.Fatalf("first lookup: %v", err)
	}
	addr2, err := c.lookup(context.Background(), "example.com", "80", "example.com:80")
	if err != nil {
		t.Fatalf("second lookup: %v", err)
	}

	if calls != 1 {
		t.Errorf("expected 1 real lookupHost call (second hits the cache), got %d", calls)
	}
	if addr1[0] != "10.0.0.1:80" || addr2[0] != "10.0.0.1:80" {
		t.Errorf("addresses = %v, %v, want both [10.0.0.1:80]", addr1, addr2)
	}
}
