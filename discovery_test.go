package relay

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// listResolver round-robins over a fixed list and records failure reports.
type listResolver struct {
	targets []string
	next    atomic.Int64

	mu       sync.Mutex
	failures []string
}

func (l *listResolver) Resolve(context.Context) (string, error) {
	i := l.next.Add(1) - 1
	return l.targets[i%int64(len(l.targets))], nil
}

func (l *listResolver) ReportFailure(target string, _ error) {
	l.mu.Lock()
	l.failures = append(l.failures, target)
	l.mu.Unlock()
}

func (l *listResolver) reported() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.failures...)
}

// deadAddr returns a loopback address with nothing listening on it, so a
// dial fails fast with "connection refused".
func deadAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func hostOf(srv *httptest.Server) string { return strings.TrimPrefix(srv.URL, "http://") }

func TestWithDiscovery_ResolvesEveryRequest(t *testing.T) {
	var hitsA, hitsB atomic.Int32
	a := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hitsA.Add(1) }))
	defer a.Close()
	b := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hitsB.Add(1) }))
	defer b.Close()

	res := &listResolver{targets: []string{hostOf(a), hostOf(b)}}
	c := New(WithBaseURL("http://placeholder.invalid"), WithDiscovery(res), WithDisableRetry())

	for i := 0; i < 4; i++ {
		resp, err := c.Execute(c.Get("/"))
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		_ = resp
	}
	if hitsA.Load() != 2 || hitsB.Load() != 2 {
		t.Fatalf("want 2/2 split, got a=%d b=%d", hitsA.Load(), hitsB.Load())
	}
	if got := res.reported(); len(got) != 0 {
		t.Fatalf("no failures expected, got %v", got)
	}
}

// A POST carrying an idempotency key is retried after a refused connection,
// the retry is re-resolved to the next instance, and the dead target is
// reported to the resolver.
func TestWithDiscovery_RetriesIdempotentPostOnAnotherInstance(t *testing.T) {
	var hits atomic.Int32
	var gotKey atomic.Value
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		gotKey.Store(r.Header.Get("X-Idempotency-Key"))
		w.WriteHeader(http.StatusOK)
	}))
	defer live.Close()

	dead := deadAddr(t)
	res := &listResolver{targets: []string{dead, hostOf(live)}}
	c := New(
		WithBaseURL("http://placeholder.invalid"),
		WithDiscovery(res),
		WithRetry(&RetryConfig{MaxAttempts: 2, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond, Multiplier: 1}),
	)

	resp, err := c.Execute(c.Post("/pay").WithIdempotencyKey("key-1").WithBody([]byte("{}")))
	if err != nil {
		t.Fatalf("expected the retry to succeed on the live instance: %v", err)
	}
	if resp.StatusCode != http.StatusOK || hits.Load() != 1 {
		t.Fatalf("status=%d hits=%d", resp.StatusCode, hits.Load())
	}
	if k, _ := gotKey.Load().(string); k != "key-1" {
		t.Fatalf("idempotency key not forwarded on retry, got %q", k)
	}
	if got := res.reported(); len(got) != 1 || got[0] != dead {
		t.Fatalf("want failure reported for %s, got %v", dead, got)
	}
}

// Without an idempotency key a POST is never retried, even with discovery.
func TestWithDiscovery_DoesNotRetryPlainPost(t *testing.T) {
	var hits atomic.Int32
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { hits.Add(1) }))
	defer live.Close()

	res := &listResolver{targets: []string{deadAddr(t), hostOf(live)}}
	c := New(
		WithBaseURL("http://placeholder.invalid"),
		WithDiscovery(res),
		WithRetry(&RetryConfig{MaxAttempts: 2, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond, Multiplier: 1}),
	)

	if _, err := c.Execute(c.Post("/pay").WithBody([]byte("{}"))); err == nil {
		t.Fatal("expected the refused connection to surface")
	}
	if hits.Load() != 0 {
		t.Fatalf("plain POST must not be retried, live hits=%d", hits.Load())
	}
}

// Cancellation is the caller's decision, not a backend failure.
func TestWithDiscovery_NoReportOnCancellation(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { <-release }))
	defer slow.Close()
	defer close(release)

	res := &listResolver{targets: []string{hostOf(slow)}}
	c := New(WithBaseURL("http://placeholder.invalid"), WithDiscovery(res), WithDisableRetry())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Execute(c.Get("/").WithContext(ctx)); err == nil {
		t.Fatal("expected a deadline error")
	}
	if got := res.reported(); len(got) != 0 {
		t.Fatalf("cancellation must not be reported, got %v", got)
	}
}

func TestWithDiscovery_ResolveErrorIsReturned(t *testing.T) {
	boom := errors.New("no instances")
	c := New(
		WithBaseURL("http://placeholder.invalid"),
		WithDiscovery(ResolverFunc(func(context.Context) (string, error) { return "", boom })),
		WithDisableRetry(),
	)
	_, err := c.Execute(c.Get("/"))
	if !errors.Is(err, boom) {
		t.Fatalf("want wrapped resolver error, got %v", err)
	}
}

func TestSRVResolver_ReportFailureDropsCache(t *testing.T) {
	var lookups atomic.Int32
	r := NewSRVResolver("", "", "svc.local", "http", WithSRVTTL(time.Hour))
	r.lookupSRV = func(context.Context, string, string, string) (string, []*net.SRV, error) {
		lookups.Add(1)
		return "", []*net.SRV{{Target: "a.local.", Port: 80}}, nil
	}

	for i := 0; i < 3; i++ {
		if _, err := r.Resolve(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if lookups.Load() != 1 {
		t.Fatalf("cached: want 1 lookup, got %d", lookups.Load())
	}

	r.ReportFailure("a.local:80", errors.New("connection refused"))
	if _, err := r.Resolve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lookups.Load() != 2 {
		t.Fatalf("after a failure report: want a fresh lookup (2), got %d", lookups.Load())
	}
}

// Compile-time checks: SRVResolver plugs into the generic discovery seam.
var (
	_ Resolver        = (*SRVResolver)(nil)
	_ FailureReporter = (*SRVResolver)(nil)
)
