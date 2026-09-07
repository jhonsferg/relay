package relay

import (
	"context"
	"net"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

// mockLookup returns a LookupSRV func that always returns the given records.
func mockLookup(records []*net.SRV) func(context.Context, string, string, string) (string, []*net.SRV, error) {
	return func(_ context.Context, _, _, _ string) (string, []*net.SRV, error) {
		return "", records, nil
	}
}

func TestSRVResolver_SelectsFirstPriority(t *testing.T) {
	records := []*net.SRV{
		{Target: "host-b.local.", Port: 8002, Priority: 2},
		{Target: "host-a.local.", Port: 8001, Priority: 1},
		{Target: "host-c.local.", Port: 8003, Priority: 3},
	}

	r := NewSRVResolver("http", "tcp", "test.local", "http",
		WithSRVBalancer(SRVPriority),
	)
	r.lookupSRV = mockLookup(records)

	got, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "host-a.local:8001"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSRVResolver_RoundRobin(t *testing.T) {
	records := []*net.SRV{
		{Target: "host-1.local.", Port: 9001, Priority: 1},
		{Target: "host-2.local.", Port: 9002, Priority: 1},
	}

	r := NewSRVResolver("http", "tcp", "test.local", "http",
		WithSRVBalancer(SRVRoundRobin),
	)
	r.lookupSRV = mockLookup(records)

	want := []string{
		"host-1.local:9001",
		"host-2.local:9002",
		"host-1.local:9001",
		"host-2.local:9002",
	}

	for i, w := range want {
		got, err := r.Resolve(context.Background())
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if got != w {
			t.Errorf("call %d: got %q, want %q", i, got, w)
		}
	}
}

func TestSRVResolver_Random(t *testing.T) {
	known := map[string]bool{
		"host-1.local:7001": true,
		"host-2.local:7002": true,
	}
	records := []*net.SRV{
		{Target: "host-1.local.", Port: 7001, Priority: 1},
		{Target: "host-2.local.", Port: 7002, Priority: 1},
	}

	r := NewSRVResolver("http", "tcp", "test.local", "http",
		WithSRVBalancer(SRVRandom),
	)
	r.lookupSRV = mockLookup(records)

	for i := range 10 {
		got, err := r.Resolve(context.Background())
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if !known[got] {
			t.Errorf("call %d: unexpected target %q", i, got)
		}
	}
}

func TestSRVResolver_TTLCache(t *testing.T) {
	var callCount atomic.Int64
	records := []*net.SRV{
		{Target: "host-cached.local.", Port: 5000, Priority: 1},
	}

	r := NewSRVResolver("http", "tcp", "test.local", "http",
		WithSRVTTL(10*time.Second),
	)
	r.lookupSRV = func(_ context.Context, _, _, _ string) (string, []*net.SRV, error) {
		callCount.Add(1)
		return "", records, nil
	}

	for range 2 {
		if _, err := r.Resolve(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if n := callCount.Load(); n != 1 {
		t.Errorf("lookupSRV called %d times, want 1", n)
	}
}

func TestWithSRVDiscovery_RewritesHost(t *testing.T) {
	ms := testutil.NewMockServer()
	defer ms.Close()
	ms.Enqueue(testutil.MockResponse{Status: 200, Body: "ok"})

	// Parse host:port from mock server URL.
	u, err := url.Parse(ms.URL())
	if err != nil {
		t.Fatalf("parse mock server url: %v", err)
	}
	mockHost := u.Hostname()
	mockPort, _ := strconv.ParseUint(u.Port(), 10, 16)

	records := []*net.SRV{
		{Target: mockHost, Port: uint16(mockPort), Priority: 1},
	}

	resolver := NewSRVResolver("http", "tcp", "svc.local", "http")
	resolver.lookupSRV = mockLookup(records)

	client := New(
		WithBaseURL("http://placeholder.local"),
		WithSRVDiscovery(resolver),
	)

	_, err = client.Execute(client.Get("/test-path"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	req, err := ms.TakeRequest(2 * time.Second)
	if err != nil {
		t.Fatalf("no request received by mock server: %v", err)
	}
	if req.Path != "/test-path" {
		t.Errorf("got path %q, want %q", req.Path, "/test-path")
	}
}

func TestSRVResolver_ConcurrentResolveNoRace(t *testing.T) {
	// Simulate a slow DNS lookup to expose any data race on r.mu during the
	// lock-release-DNS-relock sequence introduced by the fix.
	var callCount atomic.Int64
	slowLookup := func(_ context.Context, _, _, _ string) (string, []*net.SRV, error) {
		callCount.Add(1)
		time.Sleep(5 * time.Millisecond) // simulate network latency
		return "", []*net.SRV{{Target: "host1.", Port: 8080}}, nil
	}

	r := NewSRVResolver("http", "tcp", "myservice.example.com", "http",
		WithSRVTTL(50*time.Millisecond),
	)
	r.lookupSRV = slowLookup

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.Resolve(context.Background())
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("Resolve error: %v", err)
	}

	// All 20 goroutines raced the very first (cache-empty) lookup
	// simultaneously, so singleflight coalescing should collapse them into a
	// single real DNS query - see TestSRVResolver_CoalescesConcurrentMisses
	// for a more direct assertion of this property.
	if got := callCount.Load(); got != 1 {
		t.Errorf("lookupSRV called %d times, want 1 (concurrent cache-miss lookups should be coalesced)", got)
	}
}

// TestSRVResolver_CoalescesConcurrentMisses guards against a regression
// where every concurrent caller that missed the cache performed an
// independent DNS SRV lookup - a thundering herd of duplicate queries right
// when the cache expires under concurrent load, exactly the problem
// dns_cache.go's flightGroup already solved for A/AAAA lookups but
// SRVResolver didn't share.
func TestSRVResolver_CoalescesConcurrentMisses(t *testing.T) {
	var callCount atomic.Int64
	blocking := make(chan struct{})
	lookup := func(_ context.Context, _, _, _ string) (string, []*net.SRV, error) {
		callCount.Add(1)
		<-blocking // hold every concurrent caller until they've all arrived
		return "", []*net.SRV{{Target: "host1.", Port: 8080}}, nil
	}

	r := NewSRVResolver("http", "tcp", "myservice.example.com", "http")
	r.lookupSRV = lookup

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if _, err := r.Resolve(context.Background()); err != nil {
				t.Errorf("Resolve: %v", err)
			}
		}()
	}

	// Give every goroutine a chance to reach the (blocked) lookup before
	// releasing it, so they're all genuinely racing the same miss.
	time.Sleep(20 * time.Millisecond)
	close(blocking)
	wg.Wait()

	if got := callCount.Load(); got != 1 {
		t.Errorf("lookupSRV called %d times for %d concurrent callers, want 1 (coalesced)", got, goroutines)
	}
}

// TestSRVResolver_DetachesLeaderContext guards against a regression where
// the coalesced lookup ran under the triggering caller's own context: if
// that caller's context was canceled while the shared lookup was still in
// flight, every other goroutine waiting on the same coalesced call would
// receive that same cancellation error too - even ones whose own context
// was healthy. Mirrors dns_cache.go's equivalent fix.
func TestSRVResolver_DetachesLeaderContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	lookup := func(lookupCtx context.Context, _, _, _ string) (string, []*net.SRV, error) {
		// Simulate the triggering caller's own context expiring while the
		// shared lookup is still in flight.
		cancel()
		if err := lookupCtx.Err(); err != nil {
			return "", nil, err
		}
		return "", []*net.SRV{{Target: "host1.", Port: 8080}}, nil
	}

	r := NewSRVResolver("http", "tcp", "myservice.example.com", "http")
	r.lookupSRV = lookup

	if _, err := r.Resolve(ctx); err != nil {
		t.Fatalf("expected success despite the triggering context being canceled mid-flight, got: %v", err)
	}
}
