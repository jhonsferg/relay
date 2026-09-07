package oidc_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/ext/oidc"
)

// BenchmarkWithBearerToken_Static measures the per-request overhead of the
// OnBeforeRequest hook with a trivial TokenSource (no refresh logic).
func BenchmarkWithBearerToken_Static(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		oidc.WithBearerToken(oidc.StaticToken("bench-token")),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Get("/"))
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		_ = resp
	}
}

// BenchmarkWithBearerToken_RefreshingCached measures the per-request
// overhead when using RefreshingTokenSource once the token is cached (the
// steady-state hot path - no network round trip to the token endpoint).
func BenchmarkWithBearerToken_RefreshingCached(b *testing.B) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok123",
			"token_type":   "bearer",
			"expires_in":   3600,
		})
	}))
	defer tokenSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer apiSrv.Close()

	src := oidc.RefreshingTokenSource("client-id", "client-secret", tokenSrv.URL)
	client := relay.New(
		relay.WithBaseURL(apiSrv.URL),
		relay.WithDisableRetry(),
		oidc.WithBearerToken(src),
	)

	// Warm the cache so the benchmark measures the cached path, not the
	// initial network fetch.
	if _, err := client.Execute(client.Get("/")); err != nil {
		b.Fatalf("warmup: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Get("/"))
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		_ = resp
	}
}
