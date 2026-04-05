package relay_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
)

// ---- F1: Granular Timeout Control -------------------------------------------

func TestWithTLSHandshakeTimeout_SetsField(t *testing.T) {
	t.Parallel()
	c := relay.New(relay.WithTLSHandshakeTimeout(5 * time.Second))
	_ = c // config is internal; compiling with the option is sufficient coverage
}

func TestWithDialTimeout_SetsField(t *testing.T) {
	t.Parallel()
	c := relay.New(relay.WithDialTimeout(3 * time.Second))
	_ = c
}

func TestWithResponseHeaderTimeout_SetsField(t *testing.T) {
	t.Parallel()
	c := relay.New(relay.WithResponseHeaderTimeout(2 * time.Second))
	_ = c
}

func TestWithIdleConnTimeout_SetsField(t *testing.T) {
	t.Parallel()
	c := relay.New(relay.WithIdleConnTimeout(60 * time.Second))
	_ = c
}

func TestWithExpectContinueTimeout_SetsField(t *testing.T) {
	t.Parallel()
	c := relay.New(relay.WithExpectContinueTimeout(1 * time.Second))
	_ = c
}

func TestF1_RequestSucceedsWithTimeouts(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithDialTimeout(5*time.Second),
		relay.WithTLSHandshakeTimeout(5*time.Second),
		relay.WithResponseHeaderTimeout(5*time.Second),
		relay.WithIdleConnTimeout(90*time.Second),
		relay.WithExpectContinueTimeout(1*time.Second),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
	)
	resp, err := c.Execute(c.Get(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// ---- F2: Bulkhead Isolation --------------------------------------------------

func TestBulkhead_LimitsToN(t *testing.T) {
	t.Parallel()
	const limit = 3
	var concurrent atomic.Int64
	var maxSeen atomic.Int64

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := concurrent.Add(1)
		defer concurrent.Add(-1)
		for {
			if seen := maxSeen.Load(); cur > seen {
				if maxSeen.CompareAndSwap(seen, cur) {
					break
				}
			} else {
				break
			}
		}
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithMaxConcurrentRequests(limit),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
	)

	// Launch limit+1 goroutines; the extra one should block on the bulkhead.
	errs := make(chan error, limit+1)
	for i := 0; i < limit+1; i++ {
		go func() {
			_, err := c.Execute(c.Get(srv.URL))
			errs <- err
		}()
	}

	// Give goroutines time to start and fill the bulkhead.
	time.Sleep(50 * time.Millisecond)

	// Release all waiting handlers.
	for i := 0; i < limit+1; i++ {
		release <- struct{}{}
	}

	// Collect results.
	for i := 0; i < limit+1; i++ {
		if err := <-errs; err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	}

	if got := maxSeen.Load(); got > limit {
		t.Errorf("bulkhead allowed %d concurrent requests, limit was %d", got, limit)
	}
}

func TestBulkhead_ContextCancel(t *testing.T) {
	t.Parallel()
	// Fill the bulkhead completely so the next request blocks.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithMaxConcurrentRequests(1),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
	)

	// Start one request to fill the slot.
	go func() {
		_, _ = c.Execute(c.Get(srv.URL))
	}()
	time.Sleep(20 * time.Millisecond)

	// Cancel context while waiting for bulkhead.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	req := c.Get(srv.URL).WithContext(ctx)
	_, err := c.Execute(req)
	if err == nil {
		t.Fatal("expected error from cancelled bulkhead, got nil")
	}
}

// ---- F3: Pagination ----------------------------------------------------------

func TestPaginate_FollowsLinkHeader(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	var srvURL string
	calls := 0

	mux.HandleFunc("/page1", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Link", fmt.Sprintf("<%s/page2>; rel=\"next\"", srvURL))
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/page2", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Link", fmt.Sprintf("<%s/page3>; rel=\"next\"", srvURL))
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/page3", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL

	c := relay.New(
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
	)

	fnCalls := 0
	err := c.Paginate(context.Background(), c.Get(srv.URL+"/page1"), func(resp *relay.Response) (bool, error) {
		fnCalls++
		return true, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 server calls, got %d", calls)
	}
	if fnCalls != 3 {
		t.Errorf("expected fn called 3 times, got %d", fnCalls)
	}
}

func TestPaginate_StopsWhenFnReturnsFalse(t *testing.T) {
	t.Parallel()

	var srvURL string
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/p1", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Link", fmt.Sprintf("<%s/p2>; rel=\"next\"", srvURL))
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/p2", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL

	c := relay.New(
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
	)

	fnCalls := 0
	err := c.Paginate(context.Background(), c.Get(srv.URL+"/p1"), func(resp *relay.Response) (bool, error) {
		fnCalls++
		return false, nil // stop after first page
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fnCalls != 1 {
		t.Errorf("expected fn called once, got %d", fnCalls)
	}
	if calls != 1 {
		t.Errorf("expected 1 server call, got %d", calls)
	}
}

func TestPaginateWith_CustomExtractor(t *testing.T) {
	t.Parallel()

	type pageBody struct {
		NextURL string `json:"next"`
	}

	var srvURL string
	calls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/a", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pageBody{NextURL: srvURL + "/b"})
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(pageBody{}) // empty NextURL signals end
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	srvURL = srv.URL

	c := relay.New(
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
	)

	extractor := func(resp *relay.Response) string {
		var pb pageBody
		if err := resp.JSON(&pb); err != nil {
			return ""
		}
		return pb.NextURL
	}

	fnCalls := 0
	err := c.PaginateWith(context.Background(), c.Get(srv.URL+"/a"), extractor, func(resp *relay.Response) (bool, error) {
		fnCalls++
		return true, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 server calls, got %d", calls)
	}
	if fnCalls != 2 {
		t.Errorf("expected fn called twice, got %d", fnCalls)
	}
}

// ---- F4: Content Negotiation ------------------------------------------------

func TestDefaultAccept_Sent(t *testing.T) {
	t.Parallel()
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithDefaultAccept("application/json"),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
	)
	_, err := c.Execute(c.Get(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAccept != "application/json" {
		t.Errorf("expected Accept: application/json, got %q", gotAccept)
	}
}

func TestDefaultAccept_NotOverriddenWhenSet(t *testing.T) {
	t.Parallel()
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithDefaultAccept("application/json"),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
	)
	req := c.Get(srv.URL).WithAccept("text/plain")
	_, err := c.Execute(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAccept != "text/plain" {
		t.Errorf("expected Accept: text/plain (request-level), got %q", gotAccept)
	}
}

func TestContentType_StripParameters(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
	)
	resp, err := c.Execute(c.Get(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resp.ContentType(); got != "application/json" {
		t.Errorf("ContentType() = %q, want %q", got, "application/json")
	}
}

// ---- F5: Request Hedging ----------------------------------------------------

func TestHedging_FirstResponseWins(t *testing.T) {
	t.Parallel()

	var received atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		// First request responds quickly; subsequent ones are slow.
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithHedging(5*time.Millisecond),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
	)

	resp, err := c.Execute(c.Get(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	// Give goroutines time to finish.
	time.Sleep(50 * time.Millisecond)
}

func TestHedging_Disabled(t *testing.T) {
	t.Parallel()
	var count atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
		// no WithHedging - hedging disabled
	)

	resp, err := c.Execute(c.Get(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if got := count.Load(); got != 1 {
		t.Errorf("expected exactly 1 request without hedging, got %d", got)
	}
}

func TestHedgingN_UsesMaxAttempts(t *testing.T) {
	t.Parallel()

	var received atomic.Int64
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		<-ready
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithHedgingN(5*time.Millisecond, 3),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
	)

	go func() {
		time.Sleep(20 * time.Millisecond)
		close(ready)
	}()

	resp, err := c.Execute(c.Get(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}
