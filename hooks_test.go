package relay_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	relay "github.com/jhonsferg/relay"
)

// ---------------------------------------------------------------------------
// E1 - Semantic hooks
// ---------------------------------------------------------------------------

func TestWithBeforeRetryHook(t *testing.T) {
	t.Parallel()

	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var hookCalls int32
	var lastAttempt int
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithBeforeRetryHook(func(_ context.Context, attempt int, req *relay.Request, _ *http.Response, _ error) {
			atomic.AddInt32(&hookCalls, 1)
			lastAttempt = attempt
			_ = req
		}),
	)
	defer client.Shutdown(context.Background()) //nolint:errcheck

	_, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if got := atomic.LoadInt32(&hookCalls); got != 2 {
		t.Errorf("hook called %d times, want 2", got)
	}
	if lastAttempt != 2 {
		t.Errorf("lastAttempt = %d, want 2", lastAttempt)
	}
}

func TestWithBeforeRetryHook_MultipleHooks(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	var a, b int32
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithBeforeRetryHook(func(_ context.Context, _ int, _ *relay.Request, _ *http.Response, _ error) {
			atomic.AddInt32(&a, 1)
		}),
		relay.WithBeforeRetryHook(func(_ context.Context, _ int, _ *relay.Request, _ *http.Response, _ error) {
			atomic.AddInt32(&b, 1)
		}),
	)
	defer client.Shutdown(context.Background()) //nolint:errcheck

	client.Execute(client.Get("/")) //nolint:errcheck
	if av, bv := atomic.LoadInt32(&a), atomic.LoadInt32(&b); av != bv {
		t.Errorf("hooks called unequal times: a=%d b=%d", av, bv)
	}
}

func TestWithBeforeRedirectHook(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/end", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var redirectCount int32
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithBeforeRedirectHook(func(_ *http.Request, via []*http.Request) error {
			atomic.AddInt32(&redirectCount, 1)
			_ = via
			return nil
		}),
	)
	defer client.Shutdown(context.Background()) //nolint:errcheck

	resp, err := client.Execute(client.Get("/start"))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if atomic.LoadInt32(&redirectCount) != 1 {
		t.Errorf("redirectCount = %d, want 1", redirectCount)
	}
}

func TestWithBeforeRedirectHook_Abort(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/end", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sentinel := errors.New("redirect blocked")
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relay.WithBeforeRedirectHook(func(_ *http.Request, _ []*http.Request) error {
			return sentinel
		}),
	)
	defer client.Shutdown(context.Background()) //nolint:errcheck

	_, err := client.Execute(client.Get("/start"))
	if err == nil {
		t.Fatal("expected error from redirect hook, got nil")
	}
}

func TestWithOnErrorHook(t *testing.T) {
	t.Parallel()

	var hookCalled int32
	var capturedErr error

	client := relay.New(
		relay.WithBaseURL("http://127.0.0.1:1"), // unreachable
		relay.WithDisableRetry(),
		relay.WithOnErrorHook(func(_ context.Context, _ *relay.Request, err error) {
			atomic.AddInt32(&hookCalled, 1)
			capturedErr = err
		}),
	)
	defer client.Shutdown(context.Background()) //nolint:errcheck

	_, err := client.Execute(client.Get("/"))
	if err == nil {
		t.Fatal("expected error from unreachable server, got nil")
	}
	if atomic.LoadInt32(&hookCalled) != 1 {
		t.Errorf("OnErrorHook called %d times, want 1", hookCalled)
	}
	if capturedErr == nil {
		t.Error("capturedErr should not be nil")
	}
}

func TestWithOnErrorHook_NotCalledOnSuccess(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var hookCalled int32
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithOnErrorHook(func(_ context.Context, _ *relay.Request, _ error) {
			atomic.AddInt32(&hookCalled, 1)
		}),
	)
	defer client.Shutdown(context.Background()) //nolint:errcheck

	_, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if atomic.LoadInt32(&hookCalled) != 0 {
		t.Errorf("OnErrorHook called %d times, want 0 on success", hookCalled)
	}
}

// ---------------------------------------------------------------------------
// E2 - Auto idempotency on safe retries
// ---------------------------------------------------------------------------

func TestWithAutoIdempotencyOnSafeRetries_SafeMethod(t *testing.T) {
	t.Parallel()

	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Idempotency-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithAutoIdempotencyOnSafeRetries(),
	)
	defer client.Shutdown(context.Background()) //nolint:errcheck

	cases := []struct {
		req    *relay.Request
		method string
	}{
		{client.Get("/"), http.MethodGet},
		{client.Put("/"), http.MethodPut},
		{client.Options("/"), http.MethodOptions},
	}
	for _, tc := range cases {
		gotKey = ""
		_, err := client.Execute(tc.req)
		if err != nil {
			t.Fatalf("%s: Execute() error: %v", tc.method, err)
		}
		if gotKey == "" {
			t.Errorf("%s: expected X-Idempotency-Key header, got none", tc.method)
		}
	}
}

func TestWithAutoIdempotencyOnSafeRetries_UnsafeMethod(t *testing.T) {
	t.Parallel()

	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Idempotency-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithAutoIdempotencyOnSafeRetries(),
	)
	defer client.Shutdown(context.Background()) //nolint:errcheck

	for _, req := range []*relay.Request{client.Post("/"), client.Patch("/"), client.Delete("/")} {
		gotKey = ""
		_, err := client.Execute(req)
		if err != nil {
			t.Fatalf("Execute() error: %v", err)
		}
		if gotKey != "" {
			t.Errorf("unsafe method: did not expect X-Idempotency-Key, but got %q", gotKey)
		}
	}
}

func TestWithAutoIdempotencyOnSafeRetries_SameKeyOnRetry(t *testing.T) {
	t.Parallel()

	var keys []string
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("X-Idempotency-Key"))
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithAutoIdempotencyOnSafeRetries(),
	)
	defer client.Shutdown(context.Background()) //nolint:errcheck

	_, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if len(keys) < 2 {
		t.Fatalf("expected at least 2 calls, got %d", len(keys))
	}
	for i := 1; i < len(keys); i++ {
		if keys[i] != keys[0] {
			t.Errorf("idempotency key changed between retries: %q vs %q", keys[0], keys[i])
		}
	}
}

// ---------------------------------------------------------------------------
// E3 - Additional error classification helpers
// ---------------------------------------------------------------------------

func TestIsRetryableError(t *testing.T) {
	t.Parallel()

	if !relay.IsRetryableError(relay.ErrCircuitOpen, nil) {
		t.Error("ErrCircuitOpen should be retryable")
	}
	if !relay.IsRetryableError(relay.ErrMaxRetriesReached, nil) {
		t.Error("ErrMaxRetriesReached should be retryable")
	}
	if relay.IsRetryableError(nil, nil) {
		t.Error("nil error should not be retryable")
	}
}

func TestIsTimeout(t *testing.T) {
	t.Parallel()

	if !relay.IsTimeout(relay.ErrTimeout) {
		t.Error("ErrTimeout should be timeout")
	}
	if !relay.IsTimeout(context.DeadlineExceeded) {
		t.Error("context.DeadlineExceeded should be timeout")
	}
	if relay.IsTimeout(context.Canceled) {
		t.Error("context.Canceled should not be timeout")
	}
	if relay.IsTimeout(nil) {
		t.Error("nil should not be timeout")
	}
}

func TestIsCircuitOpen(t *testing.T) {
	t.Parallel()

	if !relay.IsCircuitOpen(relay.ErrCircuitOpen) {
		t.Error("ErrCircuitOpen should be circuit open")
	}
	if relay.IsCircuitOpen(relay.ErrTimeout) {
		t.Error("ErrTimeout should not be circuit open")
	}
	if relay.IsCircuitOpen(nil) {
		t.Error("nil should not be circuit open")
	}
}
