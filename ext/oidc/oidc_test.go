package oidc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/ext/oidc"
)

func TestStaticToken_InjectsHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		oidc.WithBearerToken(oidc.StaticToken("my-static-token")),
	)

	_, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "Bearer my-static-token"
	if gotAuth != want {
		t.Errorf("Authorization header = %q, want %q", gotAuth, want)
	}
}

// errorTokenSource always returns an error.
type errorTokenSource struct{ err error }

func (e *errorTokenSource) Token(_ context.Context) (string, error) { return "", e.err }

func TestWithBearerToken_PropagatesTokenError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wantErr := errors.New("token unavailable")
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		oidc.WithBearerToken(&errorTokenSource{err: wantErr}),
	)

	_, err := client.Execute(client.Get("/"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error = %v, want to wrap %v", err, wantErr)
	}
}

func TestRefreshingTokenSource_FetchesAndInjectsToken(t *testing.T) {
	// Mock token endpoint.
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok123",
			"token_type":   "bearer",
			"expires_in":   3600,
		}); err != nil {
			http.Error(w, "encode error", http.StatusInternalServerError)
		}
	}))
	defer tokenSrv.Close()

	// API server that records the Authorization header.
	var gotAuth string
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer apiSrv.Close()

	src := oidc.RefreshingTokenSource("client-id", "client-secret", tokenSrv.URL)
	client := relay.New(
		relay.WithBaseURL(apiSrv.URL),
		oidc.WithBearerToken(src),
	)

	resp, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp

	want := "Bearer tok123"
	if gotAuth != want {
		t.Errorf("Authorization header = %q, want %q", gotAuth, want)
	}
}

// TestRefreshingTokenSourceContextTimeout_BoundsTokenRefresh guards against
// a regression where token refresh had no independent timeout:
// oauthAdapter.Token drops the per-request context it's called with (the
// underlying oauth2.TokenSource.Token() interface takes no context - it's
// fixed once at construction time via clientcredentials.Config.TokenSource),
// and x/oauth2 falls back to http.DefaultClient (Timeout: 0, unbounded) when
// no HTTP client is threaded through ctx. A stalled token endpoint would
// otherwise hang every request through the client indefinitely, with no way
// for an individual caller to bound it via its own request's timeout.
func TestRefreshingTokenSourceContextTimeout_BoundsTokenRefresh(t *testing.T) {
	const tokenEndpointDelay = 500 * time.Millisecond
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(tokenEndpointDelay)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok-slow",
			"token_type":   "bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer apiSrv.Close()

	src := oidc.RefreshingTokenSourceContextTimeout(
		context.Background(), "client-id", "client-secret", tokenSrv.URL, 50*time.Millisecond,
	)
	client := relay.New(
		relay.WithBaseURL(apiSrv.URL),
		oidc.WithBearerToken(src),
	)

	// No per-request timeout and no context deadline - only the refresh
	// timeout above should bound the token fetch.
	start := time.Now()
	_, err := client.Execute(client.Get("/"))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error when the token endpoint stalls past the refresh timeout")
	}
	if elapsed >= tokenEndpointDelay {
		t.Errorf("Execute took %v, which suggests it waited for the full token-endpoint delay (%v) instead of the refresh timeout", elapsed, tokenEndpointDelay)
	}
}
