package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

func TestNew_DefaultsApplied(t *testing.T) {
	c := New()
	if c == nil {
		t.Fatal("New() returned nil")
	}
	if c.config.Timeout != defaultTimeout {
		t.Errorf("expected default timeout %v, got %v", defaultTimeout, c.config.Timeout)
	}
	if c.config.MaxRedirects != defaultMaxRedirects {
		t.Errorf("expected default max redirects %d, got %d", defaultMaxRedirects, c.config.MaxRedirects)
	}
}

func TestNew_WithOptions(t *testing.T) {
	c := New(
		WithTimeout(5*time.Second),
		WithBaseURL("https://example.com"),
		WithDefaultHeaders(map[string]string{"X-Custom": "test"}),
	)
	if c.config.Timeout != 5*time.Second {
		t.Errorf("expected 5s timeout, got %v", c.config.Timeout)
	}
	if c.config.BaseURL != "https://example.com" {
		t.Errorf("expected base URL https://example.com, got %q", c.config.BaseURL)
	}
	if c.config.DefaultHeaders["X-Custom"] != "test" {
		t.Errorf("expected default header X-Custom=test, got %q", c.config.DefaultHeaders["X-Custom"])
	}
}

func TestWith_InheritsParentConfig(t *testing.T) {
	parent := New(
		WithTimeout(10*time.Second),
		WithBaseURL("https://parent.example.com"),
		WithDefaultHeaders(map[string]string{"X-Parent": "yes"}),
	)
	child := parent.With(WithTimeout(3 * time.Second))

	if child.config.BaseURL != "https://parent.example.com" {
		t.Errorf("child should inherit parent BaseURL, got %q", child.config.BaseURL)
	}
	if child.config.Timeout != 3*time.Second {
		t.Errorf("child should override timeout to 3s, got %v", child.config.Timeout)
	}
	if child.config.DefaultHeaders["X-Parent"] != "yes" {
		t.Errorf("child should inherit parent default headers")
	}
	// Parent unchanged.
	if parent.config.Timeout != 10*time.Second {
		t.Errorf("parent timeout should remain 10s, got %v", parent.config.Timeout)
	}
}

func TestWith_IsolatesFromParent(t *testing.T) {
	parent := New(WithDefaultHeaders(map[string]string{"X-Shared": "a"}))
	child := parent.With(WithDefaultHeaders(map[string]string{"X-Shared": "b"}))

	if parent.config.DefaultHeaders["X-Shared"] != "a" {
		t.Errorf("parent header should remain 'a', got %q", parent.config.DefaultHeaders["X-Shared"])
	}
	if child.config.DefaultHeaders["X-Shared"] != "b" {
		t.Errorf("child header should be 'b', got %q", child.config.DefaultHeaders["X-Shared"])
	}
}

func TestExecute_BasicGET(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{
		Status: http.StatusOK,
		Body:   "hello world",
	})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	resp, err := c.Execute(c.Get(srv.URL() + "/hello"))
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if resp.String() != "hello world" {
		t.Errorf("expected body 'hello world', got %q", resp.String())
	}

	rec, err := srv.TakeRequest(time.Second)
	if err != nil {
		t.Fatalf("TakeRequest: %v", err)
	}
	if rec.Method != http.MethodGet {
		t.Errorf("expected GET, got %s", rec.Method)
	}
	if rec.Path != "/hello" {
		t.Errorf("expected path /hello, got %q", rec.Path)
	}
}

func TestExecute_NilRequest(t *testing.T) {
	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	_, err := c.Execute(nil)
	if err != ErrNilRequest {
		t.Errorf("expected ErrNilRequest, got %v", err)
	}
}

func TestExecute_DefaultHeadersInjected(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithDefaultHeaders(map[string]string{"X-Service": "relay-test"}),
	)
	_, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Headers.Get("X-Service") != "relay-test" {
		t.Errorf("expected X-Service header, got %q", rec.Headers.Get("X-Service"))
	}
}

func TestExecuteJSON_UnmarshalsBody(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	type payload struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	want := payload{ID: 42, Name: "Alice"}
	body, _ := json.Marshal(want)

	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    string(body),
	})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	var got payload
	resp, err := c.ExecuteJSON(c.Get(srv.URL()+"/data"), &got)
	if err != nil {
		t.Fatalf("ExecuteJSON: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if got.ID != 42 || got.Name != "Alice" {
		t.Errorf("expected {42, Alice}, got %+v", got)
	}
}

func TestExecuteJSON_NilOutSkipsUnmarshal(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: `{"x":1}`})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	resp, err := c.ExecuteJSON(c.Get(srv.URL()+"/data"), nil)
	if err != nil {
		t.Fatalf("ExecuteJSON with nil out: %v", err)
	}
	if resp == nil {
		t.Fatal("response should not be nil")
	}
}

func TestExecuteAs_Generic(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	type Item struct {
		Value string `json:"value"`
	}
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    `{"value":"hello"}`,
	})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	item, resp, err := ExecuteAs[Item](c, c.Get(srv.URL()+"/item"))
	if err != nil {
		t.Fatalf("ExecuteAs: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if item.Value != "hello" {
		t.Errorf("expected 'hello', got %q", item.Value)
	}
}

func TestExecuteBatch_AllSucceed(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	// Enqueue 3 responses.
	for i := 0; i < 3; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})
	}

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	requests := []*Request{
		c.Get(srv.URL() + "/a"),
		c.Get(srv.URL() + "/b"),
		c.Get(srv.URL() + "/c"),
	}
	results := c.ExecuteBatch(context.Background(), requests, 0)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("result[%d] error: %v", r.Index, r.Err)
		}
		if r.Response.StatusCode != http.StatusOK {
			t.Errorf("result[%d] expected 200, got %d", r.Index, r.Response.StatusCode)
		}
	}
}

func TestExecuteBatch_Empty(t *testing.T) {
	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	results := c.ExecuteBatch(context.Background(), nil, 0)
	if results != nil {
		t.Errorf("expected nil for empty batch, got %v", results)
	}
}

func TestExecuteAsync_ReceivesResult(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "async"})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	ch := c.ExecuteAsync(c.Get(srv.URL() + "/"))
	select {
	case result := <-ch:
		if result.Err != nil {
			t.Fatalf("async error: %v", result.Err)
		}
		if result.Response.StatusCode != http.StatusOK {
			t.Errorf("expected 200, got %d", result.Response.StatusCode)
		}
		if result.Response.String() != "async" {
			t.Errorf("expected body 'async', got %q", result.Response.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for async result")
	}
}

func TestExecuteAsync_ChannelClosedAfterDelivery(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	ch := c.ExecuteAsync(c.Get(srv.URL() + "/"))
	<-ch
	// Second receive should return zero value (channel closed).
	r, ok := <-ch
	if ok {
		t.Errorf("channel should be closed after delivery, got %+v", r)
	}
}

func TestExecuteAsyncCallback_OnSuccess(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "callback-ok"})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	done := make(chan struct{})
	var gotStatus int32
	c.ExecuteAsyncCallback(
		c.Get(srv.URL()+"/"),
		func(resp *Response) {
			atomic.StoreInt32(&gotStatus, int32(resp.StatusCode)) //nolint:gosec

			close(done)
		},
		func(err error) {
			t.Errorf("unexpected error callback: %v", err)
			close(done)
		},
	)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for callback")
	}
	if atomic.LoadInt32(&gotStatus) != http.StatusOK {
		t.Errorf("expected 200, got %d", atomic.LoadInt32(&gotStatus))
	}
}

func TestExecuteAsyncCallback_OnError(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.EnqueueError()

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	done := make(chan struct{})
	var mu sync.Mutex
	var gotErr error
	c.ExecuteAsyncCallback(
		c.Get(srv.URL()+"/"),
		func(resp *Response) {
			t.Error("unexpected success callback")
			close(done)
		},
		func(err error) {
			mu.Lock()
			gotErr = err
			mu.Unlock()
			close(done)
		},
	)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for error callback")
	}
	mu.Lock()
	err := gotErr
	mu.Unlock()
	if err == nil {
		t.Error("expected error from connection-close, got nil")
	}
}

func TestShutdown_RejectsNewRequests(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	ctx := context.Background()
	if err := c.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	_, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != ErrClientClosed {
		t.Errorf("expected ErrClientClosed after Shutdown, got %v", err)
	}
}

func TestShutdown_WaitsForInFlightRequests(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{
		Status: http.StatusOK,
		Delay:  100 * time.Millisecond,
	})

	// Use a channel to signal when the request has actually started.
	started := make(chan struct{})
	done := make(chan struct{})
	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithOnBeforeRequest(func(ctx context.Context, req *Request) error {
			select {
			case <-started:
			default:
				close(started)
			}
			return nil
		}),
	)

	go func() {
		_, _ = c.Execute(c.Get(srv.URL() + "/slow"))
		close(done)
	}()

	// Wait for the request to start and register in c.inFlight.
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request failed to start within 2s")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown should succeed after in-flight completes: %v", err)
	}
	<-done
}

func TestExecute_POSTWithBody(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusCreated})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Post(srv.URL() + "/items").WithJSON(map[string]string{"key": "value"})
	resp, err := c.Execute(req)
	if err != nil {
		t.Fatalf("Execute POST: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Method != http.MethodPost {
		t.Errorf("expected POST, got %s", rec.Method)
	}
	if !strings.Contains(string(rec.Body), "key") {
		t.Errorf("expected body to contain 'key', got %q", string(rec.Body))
	}
}

func TestExecute_QueryParams(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Get(srv.URL()+"/search").
		WithQueryParam("q", "relay").
		WithQueryParam("page", "2")
	_, err := c.Execute(req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Query.Get("q") != "relay" {
		t.Errorf("expected q=relay, got %q", rec.Query.Get("q"))
	}
	if rec.Query.Get("page") != "2" {
		t.Errorf("expected page=2, got %q", rec.Query.Get("page"))
	}
}

func TestIsHealthy_NoBreakerAlwaysTrue(t *testing.T) {
	c := New(WithDisableCircuitBreaker())
	if !c.IsHealthy() {
		t.Error("client without circuit breaker should always be healthy")
	}
}
