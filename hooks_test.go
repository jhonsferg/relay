package relay_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

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

// TestWithAutoIdempotencyOnSafeRetries_UnsafeMethod checks the genuinely
// non-idempotent methods (POST, PATCH). DELETE used to be included in this
// list too, asserting that it should NOT receive an auto-generated key -
// that encoded a bug: DELETE is idempotent per RFC 9110 §9.2.2 (repeating it
// has the same effect as calling it once), same as PUT, which already
// receives a key. See TestWithAutoIdempotencyOnSafeRetries_DELETE_GetsKey in
// idempotency_test.go for DELETE's corrected (positive) expectation.
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

	for _, req := range []*relay.Request{client.Post("/"), client.Patch("/")} {
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

// ---------------------------------------------------------------------------
// E5 - Redirect chain tracking
// ---------------------------------------------------------------------------

func TestRedirectChain(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			http.Redirect(w, r, "/b", http.StatusFound)
		case "/b":
			http.Redirect(w, r, "/c", http.StatusMovedPermanently)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := relay.New(relay.WithBaseURL(srv.URL))
	defer client.Shutdown(context.Background()) //nolint:errcheck

	resp, err := client.Execute(client.Get("/a"))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if !resp.WasRedirected() {
		t.Error("WasRedirected() should be true")
	}
	if got := resp.RedirectCount; got != 2 {
		t.Errorf("RedirectCount = %d, want 2", got)
	}

	chain := resp.RedirectChain()
	if len(chain) != 2 {
		t.Fatalf("RedirectChain() length = %d, want 2", len(chain))
	}

	if chain[0].StatusCode != http.StatusFound {
		t.Errorf("chain[0].StatusCode = %d, want 302", chain[0].StatusCode)
	}
	if chain[1].StatusCode != http.StatusMovedPermanently {
		t.Errorf("chain[1].StatusCode = %d, want 301", chain[1].StatusCode)
	}
}

func TestRedirectChain_NoRedirects(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New(relay.WithBaseURL(srv.URL))
	defer client.Shutdown(context.Background()) //nolint:errcheck

	resp, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if got := len(resp.RedirectChain()); got != 0 {
		t.Errorf("RedirectChain() length = %d, want 0", got)
	}
}

func TestExecute_WithDisableRedirectTracking_ZeroRedirectCount(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			http.Redirect(w, r, "/b", http.StatusFound)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := relay.New(relay.WithBaseURL(srv.URL), relay.WithDisableRedirectTracking())
	defer client.Shutdown(context.Background()) //nolint:errcheck

	resp, err := client.Execute(client.Get("/a"))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if got := resp.RedirectCount; got != 0 {
		t.Errorf("RedirectCount = %d, want 0 with tracking disabled", got)
	}
	if got := len(resp.RedirectChain()); got != 0 {
		t.Errorf("RedirectChain() length = %d, want 0 with tracking disabled", got)
	}
}

func TestExecute_WithDisableRedirectTracking_MaxRedirectsStillEnforced(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always redirect - an infinite redirect loop.
		http.Redirect(w, r, "/next", http.StatusFound)
	}))
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRedirectTracking(),
		relay.WithMaxRedirects(3),
	)
	defer client.Shutdown(context.Background()) //nolint:errcheck

	_, err := client.Execute(client.Get("/start"))
	if err == nil {
		t.Fatal("expected an error from exceeding MaxRedirects, got nil")
	}
}

func TestExecute_WithDisableRedirectTracking_BeforeRedirectHookStillRuns(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a":
			http.Redirect(w, r, "/b", http.StatusFound)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	var hookCalls int
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRedirectTracking(),
		relay.WithBeforeRedirectHook(func(_ *http.Request, _ []*http.Request) error {
			hookCalls++
			return nil
		}),
	)
	defer client.Shutdown(context.Background()) //nolint:errcheck

	_, err := client.Execute(client.Get("/a"))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if hookCalls != 1 {
		t.Errorf("BeforeRedirectHook calls = %d, want 1 even with tracking disabled", hookCalls)
	}
}

// TestRedirectChain_NoLeakBetweenRequests guards against stale entries
// leaking from one Execute call into the next now that the redirect-tracking
// state (count + chain) is pooled and reused across requests instead of
// allocated fresh every time.
func TestRedirectChain_NoLeakBetweenRequests(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/target", http.StatusFound)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := relay.New(relay.WithBaseURL(srv.URL))
	defer client.Shutdown(context.Background()) //nolint:errcheck

	redirected, err := client.Execute(client.Get("/redirect"))
	if err != nil {
		t.Fatalf("Execute(/redirect) error: %v", err)
	}
	if got := redirected.RedirectCount; got != 1 {
		t.Fatalf("RedirectCount = %d, want 1", got)
	}
	relay.PutResponse(redirected)

	plain, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("Execute(/) error: %v", err)
	}
	defer relay.PutResponse(plain)

	if got := plain.RedirectCount; got != 0 {
		t.Errorf("RedirectCount = %d, want 0 (leaked from previous request)", got)
	}
	if got := len(plain.RedirectChain()); got != 0 {
		t.Errorf("RedirectChain() length = %d, want 0 (leaked from previous request)", got)
	}
}

// TestRedirectChain_NoLeakAcrossRetryAttempts guards against a regression
// where the pooled redirectState (count + chain) is captured once per
// executeOnce call and shared, via context.WithValue, across every retry
// attempt inside it - but was only ever reset at the top of executeOnce, not
// at the top of each individual attempt. If attempt 1 redirects and then
// fails with a retryable status, and attempt 2 also redirects before
// succeeding, the final Response.RedirectChain() ended up containing hops
// from both attempts concatenated, while RedirectCount reflected only the
// last attempt - leaving the two mutually inconsistent and reporting a hop
// that happened on a discarded, failed attempt as if it were part of the
// successful response.
func TestRedirectChain_NoLeakAcrossRetryAttempts(t *testing.T) {
	t.Parallel()

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/start":
			http.Redirect(w, r, "/target", http.StatusFound)
		case "/target":
			hits++
			if hits == 1 {
				// First attempt's post-redirect response: a retryable failure.
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			// Second attempt's post-redirect response: success.
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableCircuitBreaker(),
		relay.WithRetry(&relay.RetryConfig{
			MaxAttempts:     2,
			InitialInterval: time.Millisecond,
			MaxInterval:     5 * time.Millisecond,
			Multiplier:      1.0,
			RandomFactor:    0,
			RetryableStatus: []int{http.StatusServiceUnavailable},
		}),
	)
	defer client.Shutdown(context.Background()) //nolint:errcheck

	resp, err := client.Execute(client.Get("/start"))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want 200 (after the retry)", resp.StatusCode)
	}

	if got := resp.RedirectCount; got != 1 {
		t.Errorf("RedirectCount = %d, want 1 (only the successful attempt's redirect)", got)
	}
	if got := len(resp.RedirectChain()); got != 1 {
		t.Errorf("RedirectChain() length = %d, want 1 - a hop from the failed, discarded first attempt leaked in", got)
	}
}
