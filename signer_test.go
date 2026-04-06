package relay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

// TestWithRequestSigner_Integration verifies that the signer's Sign method is
// called and its header mutation is visible to the server.
func TestWithRequestSigner_Integration(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})

	const wantKey = "test-api-key"
	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithRequestSigner(SignerFunc(func(_ context.Context, r *http.Request) error {
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

// TestWithSigner_HeaderInjected verifies backwards-compat alias WithSigner.
func TestWithSigner_HeaderInjected(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})

	const wantKey = "test-api-key"
	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithSigner(SignerFunc(func(_ context.Context, r *http.Request) error {
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
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})

	sigErr := errors.New("signing failed")
	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithRequestSigner(SignerFunc(func(_ context.Context, _ *http.Request) error {
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
	srv.EnqueueError()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})

	calls := 0
	c := New(
		WithDisableCircuitBreaker(),
		WithRetry(&RetryConfig{MaxAttempts: 2}),
		WithRequestSigner(SignerFunc(func(_ context.Context, _ *http.Request) error {
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

// TestSignerFunc_Sign verifies the SignerFunc adapter.
func TestSignerFunc_Sign(t *testing.T) {
	t.Parallel()
	called := false
	var s RequestSigner = SignerFunc(func(_ context.Context, _ *http.Request) error {
		called = true
		return nil
	})
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err := s.Sign(context.Background(), req); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !called {
		t.Error("SignerFunc was not called")
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

// TestMultiSigner_AppliesAll verifies that all signers in the chain are called.
func TestMultiSigner_AppliesAll(t *testing.T) {
	t.Parallel()
	var called []string
	a := SignerFunc(func(_ context.Context, _ *http.Request) error {
		called = append(called, "a")
		return nil
	})
	b := SignerFunc(func(_ context.Context, _ *http.Request) error {
		called = append(called, "b")
		return nil
	})
	ms := NewMultiSigner(a, b)
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err := ms.Sign(context.Background(), req); err != nil {
		t.Fatalf("MultiSigner.Sign: %v", err)
	}
	if len(called) != 2 || called[0] != "a" || called[1] != "b" {
		t.Errorf("called = %v, want [a b]", called)
	}
}

// TestMultiSigner_StopsOnError verifies that the chain stops at the first error.
func TestMultiSigner_StopsOnError(t *testing.T) {
	t.Parallel()
	errStop := errors.New("stop")
	bCalled := false
	a := SignerFunc(func(_ context.Context, _ *http.Request) error { return errStop })
	b := SignerFunc(func(_ context.Context, _ *http.Request) error {
		bCalled = true
		return nil
	})
	ms := NewMultiSigner(a, b)
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err := ms.Sign(context.Background(), req); !errors.Is(err, errStop) {
		t.Errorf("Sign error = %v, want %v", err, errStop)
	}
	if bCalled {
		t.Error("second signer should not have been called after first error")
	}
}

// TestHMACRequestSigner_SetsHeaders verifies that X-Signature and X-Timestamp
// are present after signing.
func TestHMACRequestSigner_SetsHeaders(t *testing.T) {
	t.Parallel()
	s := &HMACRequestSigner{Key: []byte("secret")}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/path", nil)
	if err := s.Sign(context.Background(), req); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if req.Header.Get("X-Signature") == "" {
		t.Error("X-Signature header not set")
	}
	if req.Header.Get("X-Timestamp") == "" {
		t.Error("X-Timestamp header not set")
	}
}

// TestHMACRequestSigner_VerifySignature confirms the HMAC-SHA256 digest matches
// a locally computed reference.
func TestHMACRequestSigner_VerifySignature(t *testing.T) {
	t.Parallel()
	key := []byte("supersecret")
	s := &HMACRequestSigner{Key: key}
	req, _ := http.NewRequest(http.MethodPost, "http://example.com/api", nil)
	if err := s.Sign(context.Background(), req); err != nil {
		t.Fatalf("Sign: %v", err)
	}

	ts := req.Header.Get("X-Timestamp")
	if ts == "" {
		t.Fatal("X-Timestamp not set")
	}

	canonical := fmt.Sprintf("%s\n%s\n%s", req.Method, req.URL.String(), ts)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(canonical))
	want := hex.EncodeToString(mac.Sum(nil))

	if got := req.Header.Get("X-Signature"); got != want {
		t.Errorf("X-Signature = %q, want %q", got, want)
	}
}

// TestHMACRequestSigner_CustomHeader verifies the Header field is honoured.
func TestHMACRequestSigner_CustomHeader(t *testing.T) {
	t.Parallel()
	s := &HMACRequestSigner{Key: []byte("k"), Header: "X-Custom-Sig"}
	req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err := s.Sign(context.Background(), req); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if req.Header.Get("X-Custom-Sig") == "" {
		t.Error("X-Custom-Sig header not set")
	}
	if req.Header.Get("X-Signature") != "" {
		t.Error("default X-Signature header should not be set when custom Header is configured")
	}
}
