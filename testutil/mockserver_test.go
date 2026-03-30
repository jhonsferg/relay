package testutil

import (
	"net/http"
	"strings"
	"testing"
	"time"

	relay "github.com/jhonsferg/relay"
)

func TestNewMockServer_StartsAndResponds(t *testing.T) {
	t.Parallel()
	srv := NewMockServer()
	defer srv.Close()

	if srv.URL() == "" {
		t.Error("URL() should return a non-empty string")
	}
	if !strings.HasPrefix(srv.URL(), "http://") {
		t.Errorf("URL() should start with http://, got %q", srv.URL())
	}
}

func TestEnqueue_TakeRequestRoundTrip(t *testing.T) {
	t.Parallel()
	srv := NewMockServer()
	defer srv.Close()

	srv.Enqueue(MockResponse{
		Status:  http.StatusCreated,
		Headers: map[string]string{"X-Custom": "value"},
		Body:    "hello testutil",
	})

	c := relay.New(
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	req := c.Post(srv.URL()+"/items").
		WithHeader("X-Request-Header", "req-value").
		WithBody([]byte("request-body"))

	resp, err := c.Execute(req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}
	if resp.String() != "hello testutil" {
		t.Errorf("expected body 'hello testutil', got %q", resp.String())
	}

	rec, err := srv.TakeRequest(time.Second)
	if err != nil {
		t.Fatalf("TakeRequest: %v", err)
	}
	if rec.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", rec.Method)
	}
	if rec.Path != "/items" {
		t.Errorf("expected path /items, got %q", rec.Path)
	}
	if rec.Headers.Get("X-Request-Header") != "req-value" {
		t.Errorf("expected X-Request-Header=req-value, got %q", rec.Headers.Get("X-Request-Header"))
	}
	if string(rec.Body) != "request-body" {
		t.Errorf("expected body 'request-body', got %q", string(rec.Body))
	}
}

func TestEnqueue_MultipleResponsesFIFO(t *testing.T) {
	t.Parallel()
	srv := NewMockServer()
	defer srv.Close()

	srv.Enqueue(
		MockResponse{Status: http.StatusOK, Body: "first"},
		MockResponse{Status: http.StatusAccepted, Body: "second"},
		MockResponse{Status: http.StatusNoContent},
	)

	c := relay.New(relay.WithDisableRetry(), relay.WithDisableCircuitBreaker())
	expected := []int{http.StatusOK, http.StatusAccepted, http.StatusNoContent}

	for i, wantStatus := range expected {
		resp, err := c.Execute(c.Get(srv.URL() + "/"))
		if err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
		if resp.StatusCode != wantStatus {
			t.Errorf("request %d: expected %d, got %d", i, wantStatus, resp.StatusCode)
		}
	}
}

func TestEnqueueError_CausesConnectionError(t *testing.T) {
	t.Parallel()
	srv := NewMockServer()
	defer srv.Close()

	srv.EnqueueError()

	c := relay.New(relay.WithDisableRetry(), relay.WithDisableCircuitBreaker())
	_, err := c.Execute(c.Get(srv.URL() + "/broken"))
	if err == nil {
		t.Fatal("expected connection error from EnqueueError, got nil")
	}
}

func TestRequestCount_IncrementsCorrectly(t *testing.T) {
	t.Parallel()
	srv := NewMockServer()
	defer srv.Close()

	if srv.RequestCount() != 0 {
		t.Errorf("initial RequestCount should be 0, got %d", srv.RequestCount())
	}

	c := relay.New(relay.WithDisableRetry(), relay.WithDisableCircuitBreaker())

	for i := 1; i <= 5; i++ {
		srv.Enqueue(MockResponse{Status: http.StatusOK})
		_, err := c.Execute(c.Get(srv.URL() + "/"))
		if err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
		if srv.RequestCount() != i {
			t.Errorf("after %d requests, RequestCount=%d", i, srv.RequestCount())
		}
	}
}

func TestRequestCount_IncludesErrors(t *testing.T) {
	t.Parallel()
	srv := NewMockServer()
	defer srv.Close()

	srv.EnqueueError()
	srv.Enqueue(MockResponse{Status: http.StatusOK})

	c := relay.New(relay.WithDisableRetry(), relay.WithDisableCircuitBreaker())

	c.Execute(c.Get(srv.URL() + "/")) //nolint:errcheck
	c.Execute(c.Get(srv.URL() + "/")) //nolint:errcheck

	// Both requests hit the server (even the one that caused an error).
	if srv.RequestCount() != 2 {
		t.Errorf("expected RequestCount=2, got %d", srv.RequestCount())
	}
}

func TestTakeRequest_BlocksUntilRequestArrives(t *testing.T) {
	t.Parallel()
	srv := NewMockServer()
	defer srv.Close()

	srv.Enqueue(MockResponse{Status: http.StatusOK})

	c := relay.New(relay.WithDisableRetry(), relay.WithDisableCircuitBreaker())

	// TakeRequest before the request fires - it should block briefly.
	go func() {
		time.Sleep(20 * time.Millisecond)
		c.Execute(c.Get(srv.URL() + "/")) //nolint:errcheck
	}()

	rec, err := srv.TakeRequest(2 * time.Second)
	if err != nil {
		t.Fatalf("TakeRequest timed out: %v", err)
	}
	if rec == nil {
		t.Error("expected non-nil recorded request")
	}
}

func TestTakeRequest_TimeoutWhenNoRequest(t *testing.T) {
	t.Parallel()
	srv := NewMockServer()
	defer srv.Close()

	// Don't send any request - TakeRequest should time out.
	_, err := srv.TakeRequest(50 * time.Millisecond)
	if err == nil {
		t.Error("expected timeout error when no request arrives")
	}
}

func TestTakeRequest_FIFOOrder(t *testing.T) {
	t.Parallel()
	srv := NewMockServer()
	defer srv.Close()

	srv.Enqueue(MockResponse{Status: http.StatusOK})
	srv.Enqueue(MockResponse{Status: http.StatusOK})
	srv.Enqueue(MockResponse{Status: http.StatusOK})

	c := relay.New(relay.WithDisableRetry(), relay.WithDisableCircuitBreaker())

	paths := []string{"/alpha", "/beta", "/gamma"}
	for _, p := range paths {
		_, err := c.Execute(c.Get(srv.URL() + p))
		if err != nil {
			t.Fatalf("Execute %s: %v", p, err)
		}
	}

	for i, want := range paths {
		rec, err := srv.TakeRequest(time.Second)
		if err != nil {
			t.Fatalf("TakeRequest %d: %v", i, err)
		}
		if rec.Path != want {
			t.Errorf("request %d: expected path %q, got %q", i, want, rec.Path)
		}
	}
}

func TestMockServer_DefaultResponseWhenQueueEmpty(t *testing.T) {
	t.Parallel()
	srv := NewMockServer()
	defer srv.Close()

	// Don't enqueue anything - server should return 200 OK by default.
	c := relay.New(relay.WithDisableRetry(), relay.WithDisableCircuitBreaker())
	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected default 200, got %d", resp.StatusCode)
	}
}

func TestMockServer_DelayHonored(t *testing.T) {
	t.Parallel()
	srv := NewMockServer()
	defer srv.Close()

	delay := 50 * time.Millisecond
	srv.Enqueue(MockResponse{Status: http.StatusOK, Delay: delay})

	c := relay.New(relay.WithDisableRetry(), relay.WithDisableCircuitBreaker())

	start := time.Now()
	_, err := c.Execute(c.Get(srv.URL() + "/slow"))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if elapsed < delay {
		t.Errorf("expected at least %v delay, got %v", delay, elapsed)
	}
}

func TestMockServer_QueryParamsRecorded(t *testing.T) {
	t.Parallel()
	srv := NewMockServer()
	defer srv.Close()
	srv.Enqueue(MockResponse{Status: http.StatusOK})

	c := relay.New(relay.WithDisableRetry(), relay.WithDisableCircuitBreaker())
	_, err := c.Execute(c.Get(srv.URL()+"/search").
		WithQueryParam("q", "relay").
		WithQueryParam("page", "3"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rec, err := srv.TakeRequest(time.Second)
	if err != nil {
		t.Fatalf("TakeRequest: %v", err)
	}
	if rec.Query.Get("q") != "relay" {
		t.Errorf("expected q=relay, got %q", rec.Query.Get("q"))
	}
	if rec.Query.Get("page") != "3" {
		t.Errorf("expected page=3, got %q", rec.Query.Get("page"))
	}
}
