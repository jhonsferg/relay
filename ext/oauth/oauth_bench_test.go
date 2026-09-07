package oauth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhonsferg/relay"
	relayoauth "github.com/jhonsferg/relay/ext/oauth"
)

// BenchmarkWithClientCredentials_CachedToken measures the steady-state
// per-request overhead of roundTripper.RoundTrip once a token is cached -
// the hot path for every outgoing request (tokenSource.get's RLock fast
// path, request Clone, and Authorization header injection). The token
// endpoint is hit once up front; ExpiresIn is generous so no refresh occurs
// during the benchmark loop.
func BenchmarkWithClientCredentials_CachedToken(b *testing.B) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"access_token":"bench-token","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer apiSrv.Close()

	c := relay.New(
		relay.WithBaseURL(apiSrv.URL),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relayoauth.WithClientCredentials(relayoauth.Config{
			TokenURL:     tokenSrv.URL + "/token",
			ClientID:     "bench-client",
			ClientSecret: "bench-secret",
		}),
	)

	// Prime the cache so the loop below only exercises the cached-token path.
	if _, err := c.Execute(c.Get("/warmup")); err != nil {
		b.Fatalf("warmup Execute: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		resp, err := c.Execute(c.Get("/"))
		if err != nil {
			b.Fatalf("Execute: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("status = %d, want 200", resp.StatusCode)
		}
	}
}
