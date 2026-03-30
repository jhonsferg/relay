package relay

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

func TestStreamResponse_StatusHelpers(t *testing.T) {
	cases := []struct {
		code      int
		success   bool
		isErr     bool
		clientErr bool
		serverErr bool
	}{
		{200, true, false, false, false},
		{201, true, false, false, false},
		{301, false, false, false, false},
		{400, false, true, true, false},
		{404, false, true, true, false},
		{500, false, true, false, true},
		{503, false, true, false, true},
	}
	for _, tc := range cases {
		s := &StreamResponse{StatusCode: tc.code, Headers: http.Header{}}
		if s.IsSuccess() != tc.success {
			t.Errorf("[%d] IsSuccess: got %v, want %v", tc.code, s.IsSuccess(), tc.success)
		}
		if s.IsError() != tc.isErr {
			t.Errorf("[%d] IsError: got %v, want %v", tc.code, s.IsError(), tc.isErr)
		}
		if s.IsClientError() != tc.clientErr {
			t.Errorf("[%d] IsClientError: got %v, want %v", tc.code, s.IsClientError(), tc.clientErr)
		}
		if s.IsServerError() != tc.serverErr {
			t.Errorf("[%d] IsServerError: got %v, want %v", tc.code, s.IsServerError(), tc.serverErr)
		}
	}
}

func TestStreamResponse_Header(t *testing.T) {
	h := http.Header{}
	h.Set("X-Custom", "value")
	s := &StreamResponse{StatusCode: 200, Headers: h}
	if s.Header("X-Custom") != "value" {
		t.Errorf("expected 'value', got %q", s.Header("X-Custom"))
	}
}

func TestStreamResponse_ContentType(t *testing.T) {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	s := &StreamResponse{StatusCode: 200, Headers: h}
	if s.ContentType() != "application/json" {
		t.Errorf("expected 'application/json', got %q", s.ContentType())
	}
}

func TestExecuteStream_Basic(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Body:    "streaming body content",
		Headers: map[string]string{"Content-Type": "text/plain"},
	})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	stream, err := c.ExecuteStream(c.Get(srv.URL() + "/stream"))
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	defer func() { _ = stream.Body.Close() }() //nolint:errcheck

	if stream.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", stream.StatusCode)
	}
	body, _ := io.ReadAll(stream.Body)
	if string(body) != "streaming body content" {
		t.Errorf("expected 'streaming body content', got %q", string(body))
	}
}

func TestExecuteStream_NilRequest(t *testing.T) {
	c := New()
	_, err := c.ExecuteStream(nil)
	if err != ErrNilRequest {
		t.Errorf("expected ErrNilRequest, got %v", err)
	}
}

func TestExecuteStream_AfterShutdown(t *testing.T) {
	c := New()
	c.Shutdown(context.Background()) //nolint:errcheck
	_, err := c.ExecuteStream(c.Get("http://example.com/"))
	if err != ErrClientClosed {
		t.Errorf("expected ErrClientClosed, got %v", err)
	}
}

func TestExecuteStream_WithRateLimit_Cancel(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	c := New(WithRateLimit(0.001, 1), WithDisableRetry(), WithDisableCircuitBreaker())
	// Consume the burst token.
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})
	stream, err := c.ExecuteStream(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("first stream: %v", err)
	}
	_ = stream.Body.Close() //nolint:errcheck

	// Second request must wait - cancel quickly via timeout.
	req := c.Get(srv.URL() + "/").WithTimeout(10 * time.Millisecond)
	_, err = c.ExecuteStream(req)
	if err == nil {
		t.Error("expected rate-limit wait to be canceled")
	}
}

func TestManagedReadCloser_ClosedOnce(t *testing.T) {
	closedCount := 0
	rc := io.NopCloser(io.LimitReader(nil, 0))
	m := &managedReadCloser{
		ReadCloser: rc,
		cleanups:   []func(){func() { closedCount++ }},
	}
	m.Close() //nolint:errcheck
	m.Close() //nolint:errcheck
	m.Close() //nolint:errcheck
	if closedCount != 1 {
		t.Errorf("expected cleanup called once, got %d", closedCount)
	}
}

func TestManagedReadCloser_NilCleanupSkipped(t *testing.T) {
	rc := io.NopCloser(io.LimitReader(nil, 0))
	m := &managedReadCloser{
		ReadCloser: rc,
		cleanups:   []func(){nil, nil},
	}
	// Should not panic.
	if err := m.Close(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWithCustomDialer(t *testing.T) {
	c := New(WithCustomDialer(nil))
	if c == nil {
		t.Fatal("New with WithCustomDialer returned nil")
	}
}
