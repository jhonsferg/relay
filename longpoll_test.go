package relay_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
)

func longPollServer(responseStatus int, responseBody string, etag string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag && etag != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(responseStatus)
		_, _ = w.Write([]byte(responseBody))
	}))
}

func TestExecuteLongPoll_ModifiedResponse(t *testing.T) {
	srv := longPollServer(http.StatusOK, "new data", "etag-123")
	defer srv.Close()

	client := relay.New()
	ctx := context.Background()

	result, err := client.ExecuteLongPoll(ctx, client.Get(srv.URL), "", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Modified {
		t.Errorf("Modified = %v, want true", result.Modified)
	}
	if result.Response == nil {
		t.Fatal("Response is nil, want non-nil")
	}
	if result.ETag != "etag-123" {
		t.Errorf("ETag = %q, want %q", result.ETag, "etag-123")
	}
	body := result.Response.String()
	if body != "new data" {
		t.Errorf("response body = %q, want %q", body, "new data")
	}
}

func TestExecuteLongPoll_NotModified(t *testing.T) {
	srv := longPollServer(http.StatusOK, "old data", "etag-456")
	defer srv.Close()

	client := relay.New()
	ctx := context.Background()

	result, err := client.ExecuteLongPoll(ctx, client.Get(srv.URL), "etag-456", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Modified {
		t.Errorf("Modified = %v, want false", result.Modified)
	}
	if result.Response != nil {
		t.Fatal("Response is not nil, want nil")
	}
	if result.ETag != "etag-456" {
		t.Errorf("ETag = %q, want %q", result.ETag, "etag-456")
	}
}

func TestExecuteLongPoll_FirstPoll(t *testing.T) {
	srv := longPollServer(http.StatusOK, "initial data", "etag-first")
	defer srv.Close()

	client := relay.New()
	ctx := context.Background()

	result, err := client.ExecuteLongPoll(ctx, client.Get(srv.URL), "", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Modified {
		t.Errorf("Modified = %v, want true", result.Modified)
	}
	if result.ETag != "etag-first" {
		t.Errorf("ETag = %q, want %q", result.ETag, "etag-first")
	}
}

func TestExecuteLongPoll_ContextCancellation(t *testing.T) {
	srv := longPollServer(http.StatusOK, "data", "etag-789")
	defer srv.Close()

	client := relay.New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.ExecuteLongPoll(ctx, client.Get(srv.URL), "", 5*time.Second)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestExecuteLongPoll_IfNoneMatchHeader(t *testing.T) {
	headerReceived := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headerReceived = r.Header.Get("If-None-Match")
		w.Header().Set("ETag", "etag-test")
		if headerReceived == "etag-test" {
			w.WriteHeader(http.StatusNotModified)
		} else {
			w.Header().Set("ETag", "etag-new")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data"))
		}
	}))
	defer srv.Close()

	client := relay.New()
	ctx := context.Background()

	result, err := client.ExecuteLongPoll(ctx, client.Get(srv.URL), "etag-test", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if headerReceived != "etag-test" {
		t.Errorf("If-None-Match header = %q, want %q", headerReceived, "etag-test")
	}
	if result.Modified {
		t.Errorf("Modified = %v, want false", result.Modified)
	}
}

func TestExecuteLongPoll_ResponseHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", "etag-value")
		w.Header().Set("X-Custom", "custom-value")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test data"))
	}))
	defer srv.Close()

	client := relay.New()
	ctx := context.Background()

	result, err := client.ExecuteLongPoll(ctx, client.Get(srv.URL), "", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Modified {
		t.Errorf("Modified = %v, want true", result.Modified)
	}
	if result.Response.Header("X-Custom") != "custom-value" {
		t.Errorf("Custom header = %q, want %q", result.Response.Header("X-Custom"), "custom-value")
	}
}

func TestExecuteLongPoll_NilRequest(t *testing.T) {
	client := relay.New()
	ctx := context.Background()

	_, err := client.ExecuteLongPoll(ctx, nil, "", 5*time.Second)
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
}
