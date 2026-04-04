package relay

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

// TestWithSigner_HeaderInjected verifies that the signer's Sign method is
// called and its header mutation is visible to the server.
func TestWithSigner_HeaderInjected(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})

	const wantKey = "test-api-key"
	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithSigner(RequestSignerFunc(func(r *http.Request) error {
			r.Header.Set("X-Api-Key", wantKey)
			return nil
		})),
	)

	_, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	req, err := srv.TakeRequest(time.Second)
	if err != nil {
		t.Fatalf("TakeRequest: %v", err)
	}
	if got := req.Headers.Get("X-Api-Key"); got != wantKey {
		t.Errorf("X-Api-Key = %q, want %q", got, wantKey)
	}
}

// TestWithSigner_ErrorAborts verifies that a non-nil error from Sign aborts
// the request and is returned (wrapped) from Execute.
func TestWithSigner_ErrorAborts(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	// No response queued - should never be reached.
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})

	sigErr := errors.New("signing failed")
	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithSigner(RequestSignerFunc(func(_ *http.Request) error {
			return sigErr
		})),
	)

	_, err := c.Execute(c.Get(srv.URL() + "/"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sigErr) {
		t.Errorf("error = %v, want to contain %v", err, sigErr)
	}
}

// TestWithSigner_CalledOnRetry verifies that Sign is called on each attempt,
// not just the first.
func TestWithSigner_CalledOnRetry(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	// First attempt fails, second succeeds.
	srv.EnqueueError()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})

	calls := 0
	c := New(
		WithDisableCircuitBreaker(),
		WithRetry(&RetryConfig{MaxAttempts: 2}),
		WithSigner(RequestSignerFunc(func(r *http.Request) error {
			calls++
			return nil
		})),
	)

	_, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if calls < 2 {
		t.Errorf("Sign called %d times, want >= 2 (at least once per attempt)", calls)
	}
}

// TestRequestSignerFunc_Sign verifies the RequestSignerFunc adapter.
func TestRequestSignerFunc_Sign(t *testing.T) {
	t.Parallel()
	called := false
	var s RequestSigner = RequestSignerFunc(func(r *http.Request) error {
		called = true
		return nil
	})
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err := s.Sign(req); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !called {
		t.Error("RequestSignerFunc was not called")
	}
}

// TestWithSigner_NoSigner verifies that Execute works correctly when no signer
// is configured (nil check in Execute path).
func TestWithSigner_NoSigner(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	_, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("unexpected error without signer: %v", err)
	}
}
