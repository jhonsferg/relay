package relay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

// TestMultiSigner_AppliesAll verifies that every signer in the chain is called.
func TestMultiSigner_AppliesAll(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)

	var calls []string
	ms := NewMultiSigner(
		RequestSignerFunc(func(r *http.Request) error { calls = append(calls, "a"); return nil }),
		RequestSignerFunc(func(r *http.Request) error { calls = append(calls, "b"); return nil }),
	)

	if err := ms.Sign(req); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(calls) != 2 || calls[0] != "a" || calls[1] != "b" {
		t.Errorf("calls = %v, want [a b]", calls)
	}
}

// TestMultiSigner_StopsOnError verifies the chain halts at the first error.
func TestMultiSigner_StopsOnError(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)

	sentinel := errors.New("signer b failed")
	called := false
	ms := NewMultiSigner(
		RequestSignerFunc(func(_ *http.Request) error { return sentinel }),
		RequestSignerFunc(func(_ *http.Request) error { called = true; return nil }),
	)

	err := ms.Sign(req)
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want %v", err, sentinel)
	}
	if called {
		t.Error("second signer should not have been called after first error")
	}
}

// TestMultiSigner_SkipsNil verifies that nil signers are ignored.
func TestMultiSigner_SkipsNil(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	ms := NewMultiSigner(nil, nil)
	if err := ms.Sign(req); err != nil {
		t.Fatalf("unexpected error with all-nil chain: %v", err)
	}
}

// TestHMACRequestSigner_SetsHeaders verifies that X-Timestamp and X-Signature
// are present after signing.
func TestHMACRequestSigner_SetsHeaders(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/api", nil)
	s := &HMACRequestSigner{Key: []byte("secret")}
	if err := s.Sign(req); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if req.Header.Get("X-Timestamp") == "" {
		t.Error("X-Timestamp header not set")
	}
	if req.Header.Get("X-Signature") == "" {
		t.Error("X-Signature header not set")
	}
}

// TestHMACRequestSigner_VerifySignature recomputes the expected HMAC and
// compares it with the header set by Sign.
func TestHMACRequestSigner_VerifySignature(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest(http.MethodPost, "http://example.com/submit", nil)
	key := []byte("my-secret-key")
	s := &HMACRequestSigner{Key: key}
	if err := s.Sign(req); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	ts := req.Header.Get("X-Timestamp")
	canonical := "POST\n" + "http://example.com/submit" + "\n" + ts
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(canonical))
	want := hex.EncodeToString(mac.Sum(nil))
	if got := req.Header.Get("X-Signature"); got != want {
		t.Errorf("X-Signature = %q, want %q", got, want)
	}
}

// TestHMACRequestSigner_CustomHeader verifies that the Header field overrides
// the default "X-Signature" name.
func TestHMACRequestSigner_CustomHeader(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	s := &HMACRequestSigner{Key: []byte("k"), Header: "X-My-Sig"}
	if err := s.Sign(req); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if req.Header.Get("X-My-Sig") == "" {
		t.Error("X-My-Sig header not set")
	}
	if req.Header.Get("X-Signature") != "" {
		t.Error("default X-Signature should not be set when Header is overridden")
	}
}

// TestHMACRequestSigner_EmptyKey verifies that an empty key returns an error.
func TestHMACRequestSigner_EmptyKey(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/", nil)
	s := &HMACRequestSigner{}
	if err := s.Sign(req); err == nil {
		t.Error("expected error for empty key, got nil")
	}
}
