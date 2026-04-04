package testutil_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/testutil"
)

func TestRequestRecorder_NewIsEmpty(t *testing.T) {
	rec := testutil.NewRequestRecorder()
	if len(rec.Requests()) != 0 {
		t.Errorf("expected empty requests, got %d", len(rec.Requests()))
	}
}

func TestRequestRecorder_RecordsRequest(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: 200, Body: "ok"})

	rec := testutil.NewRequestRecorder()
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithTransportMiddleware(rec.Middleware()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)
	_, err := c.Execute(c.Get("/hello"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	reqs := rec.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 recorded request, got %d", len(reqs))
	}
	if reqs[0].Method != http.MethodGet {
		t.Errorf("Method = %s, want GET", reqs[0].Method)
	}
	if !strings.Contains(reqs[0].URL, "/hello") {
		t.Errorf("URL = %s, expected to contain /hello", reqs[0].URL)
	}
}

func TestRequestRecorder_RecordsBody(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: 201, Body: "{}"})

	rec := testutil.NewRequestRecorder()
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithTransportMiddleware(rec.Middleware()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)
	_, err := c.Execute(c.Post("/items").WithBody([]byte(`{"name":"test"}`)))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	reqs := rec.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].Method != http.MethodPost {
		t.Errorf("Method = %s, want POST", reqs[0].Method)
	}
	if !strings.Contains(string(reqs[0].Body), "test") {
		t.Errorf("Body = %s, expected to contain 'test'", string(reqs[0].Body))
	}
}

func TestRequestRecorder_MultipleRequests(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	for i := 0; i < 3; i++ {
		srv.Enqueue(testutil.MockResponse{Status: 200})
	}

	rec := testutil.NewRequestRecorder()
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithTransportMiddleware(rec.Middleware()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)
	for i := 0; i < 3; i++ {
		_, _ = c.Execute(c.Get("/path"))
	}
	if len(rec.Requests()) != 3 {
		t.Errorf("expected 3 requests, got %d", len(rec.Requests()))
	}
}

func TestRequestRecorder_Reset(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	for i := 0; i < 2; i++ {
		srv.Enqueue(testutil.MockResponse{Status: 200})
	}

	rec := testutil.NewRequestRecorder()
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithTransportMiddleware(rec.Middleware()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)
	_, _ = c.Execute(c.Get("/"))
	if len(rec.Requests()) != 1 {
		t.Fatalf("expected 1 request before reset, got %d", len(rec.Requests()))
	}
	rec.Reset()
	if len(rec.Requests()) != 0 {
		t.Errorf("expected 0 requests after reset, got %d", len(rec.Requests()))
	}
	// Verify new requests are recorded after reset.
	_, _ = c.Execute(c.Get("/"))
	if len(rec.Requests()) != 1 {
		t.Errorf("expected 1 request after reset+request, got %d", len(rec.Requests()))
	}
}

func TestRequestRecorder_RecordsHeaders(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: 200})

	rec := testutil.NewRequestRecorder()
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithTransportMiddleware(rec.Middleware()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)
	_, _ = c.Execute(c.Get("/").WithHeader("X-Custom", "value123"))
	reqs := rec.Requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].Headers.Get("X-Custom") != "value123" {
		t.Errorf("X-Custom header = %q, want value123", reqs[0].Headers.Get("X-Custom"))
	}
}
