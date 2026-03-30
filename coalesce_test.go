package relay

import (
	"net/http"
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
