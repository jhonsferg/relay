package relay

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

func TestCoalesce_ConcurrentRequestsProduceSingleRealRequest(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	// Serve a slow response so both goroutines are in-flight simultaneously.
	srv.Enqueue(testutil.MockResponse{
		Status: http.StatusOK,
		Body:   "coalesced-body",
		Delay:  40 * time.Millisecond,
	})

	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithRequestCoalescing(),
	)

	const goroutines = 5
	results := make([]string, goroutines)
	errs := make([]error, goroutines)

	var wg sync.WaitGroup
	// Use a barrier so all goroutines start as close together as possible.
	barrier := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			resp, err := c.Execute(c.Get(srv.URL() + "/shared"))
			errs[i] = err
			if resp != nil {
				results[i] = resp.String()
			}
		}()
	}

	close(barrier)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d error: %v", i, err)
		}
	}

	// Only 1 real request should have reached the server.
	if srv.RequestCount() != 1 {
		t.Errorf("expected 1 server request (coalescing), got %d", srv.RequestCount())
	}
}

func TestCoalesce_EachGoroutineGetsOwnBodyCopy(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{
		Status: http.StatusOK,
		Body:   "shared-response",
		Delay:  40 * time.Millisecond,
	})

	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithRequestCoalescing(),
	)

	const goroutines = 4
	bodies := make([]string, goroutines)
	var wg sync.WaitGroup
	barrier := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			resp, err := c.Execute(c.Get(srv.URL() + "/shared"))
			if err != nil {
				return
			}
			bodies[i] = resp.String()
		}()
	}

	close(barrier)
	wg.Wait()

	for i, body := range bodies {
		if body != "shared-response" {
			t.Errorf("goroutine %d got wrong body %q, expected 'shared-response'", i, body)
		}
	}
}

// TestCoalesce_ResponseHeadersNotSharedAcrossCallers guards against
// coalesceTransport.RoundTrip building each caller's *http.Response via
// `cloned := *r.resp` - a shallow struct copy that leaves cloned.Header
// aliasing the exact same http.Header map singleflight.Do handed to every
// coalesced caller, instead of cloning it per caller the way
// deduplicator.RoundTrip (singleflight.go) already does for its own,
// separate GET/HEAD dedup path (`Header: r.header.Clone()`). That aliased
// map flows straight into each caller's own Response.Headers
// (newResponse's `r.Headers = resp.Header` in response.go), so any caller
// mutating its own Response.Headers - a normal http.Header, whose exported
// Set/Add/Del methods relay never disallows - silently mutates every other
// concurrently-coalesced caller's Response.Headers too.
func TestCoalesce_ResponseHeadersNotSharedAcrossCallers(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Body:    "shared-response",
		Headers: map[string]string{"X-Original": "value"},
		Delay:   40 * time.Millisecond,
	})

	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithRequestCoalescing(),
	)

	const goroutines = 2
	resps := make([]*Response, goroutines)
	var wg sync.WaitGroup
	barrier := make(chan struct{})

	for i := 0; i < goroutines; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			resp, err := c.Execute(c.Get(srv.URL() + "/shared"))
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
				return
			}
			resps[i] = resp
		}()
	}

	close(barrier)
	wg.Wait()

	if resps[0] == nil || resps[1] == nil {
		t.Fatal("expected both goroutines to get a response")
	}

	resps[0].Headers.Set("X-Mutated-By-Caller-0", "yes")

	if got := resps[1].Headers.Get("X-Mutated-By-Caller-0"); got != "" {
		t.Errorf("caller 1's Headers picked up caller 0's mutation (got %q) - coalesceTransport shares one http.Header map across all coalesced callers instead of cloning it per caller", got)
	}
}

func TestCoalesce_DifferentURLsNotCoalesced(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	// Enqueue responses for two distinct URLs.
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "a", Delay: 30 * time.Millisecond})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "b", Delay: 30 * time.Millisecond})

	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithRequestCoalescing(),
	)

	var wg sync.WaitGroup
	barrier := make(chan struct{})

	for _, path := range []string{"/a", "/b"} {
		path := path
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			c.Execute(c.Get(srv.URL() + path)) //nolint:errcheck
		}()
	}

	close(barrier)
	wg.Wait()

	if srv.RequestCount() != 2 {
		t.Errorf("different URLs should not be coalesced; expected 2 requests, got %d", srv.RequestCount())
	}
}

func TestCoalesce_POSTNotCoalesced(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Delay: 30 * time.Millisecond})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Delay: 30 * time.Millisecond})

	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithRequestCoalescing(),
	)

	var count int32
	var wg sync.WaitGroup
	barrier := make(chan struct{})

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-barrier
			_, err := c.Execute(c.Post(srv.URL() + "/post").WithBody([]byte("data")))
			if err == nil {
				atomic.AddInt32(&count, 1)
			}
		}()
	}

	close(barrier)
	wg.Wait()

	// Both POST requests should have reached the server (no coalescing).
	if srv.RequestCount() != 2 {
		t.Errorf("POST requests should not be coalesced; expected 2, got %d", srv.RequestCount())
	}
}

// TestCoalesceTransport_DetachesLeaderContext guards against a regression
// where the shared (leader) request ran under the triggering caller's own
// context. If that caller's context expired or was canceled while the
// shared network call was still in flight, every other goroutine waiting on
// the same coalesced key would receive that same cancellation error too -
// even ones whose own context was perfectly healthy. singleflight.go's
// deduplicator.RoundTrip already solves this via a detached context;
// coalesceTransport.RoundTrip must do the same.
func TestCoalesceTransport_DetachesLeaderContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	base := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		// Simulate the triggering caller's own context expiring while the
		// shared request is still in flight.
		cancel()
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("ok")),
		}, nil
	})

	transport := newCoalesceTransport(base)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/x", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("expected success despite the triggering context being canceled mid-flight, got: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestCoalesce_SequentialRequestsAreSeparate(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "first"})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "second"})

	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithRequestCoalescing(),
	)

	resp1, err := c.Execute(c.Get(srv.URL() + "/seq"))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	resp2, err := c.Execute(c.Get(srv.URL() + "/seq"))
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	// Sequential requests are NOT coalesced because the first completed before
	// the second started.
	if srv.RequestCount() != 2 {
		t.Errorf("sequential requests should each reach server; got %d", srv.RequestCount())
	}
	_ = resp1
	_ = resp2
}
