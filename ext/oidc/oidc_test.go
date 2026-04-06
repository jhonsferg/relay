package oidc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
