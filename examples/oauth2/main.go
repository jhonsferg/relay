// Package main demonstrates the relay OAuth2 Client Credentials extension.
// The client fetches and caches access tokens automatically; callers
// simply make normal requests and the Bearer header is injected transparently.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	relay "github.com/jhonsferg/relay"
	relayoauth "github.com/jhonsferg/relay/ext/oauth"
)

func main() {
	// ---------------------------------------------------------------------------
	// Fake token endpoint: issues a JWT-like token valid for 60 seconds.
	// In production this would be Keycloak, Auth0, Okta, Azure AD, etc.
	// ---------------------------------------------------------------------------
	var tokenRequestCount atomic.Int32

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Validate the client credentials sent in the POST body.
		grantType := r.FormValue("grant_type")
		clientID := r.FormValue("client_id")
		clientSecret := r.FormValue("client_secret")

		if grantType != "client_credentials" || clientID != "my-client-id" || clientSecret != "s3cr3t" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{ //nolint:errcheck
				"error":             "invalid_client",
				"error_description": "client authentication failed",
			})
			return
		}

		tokenRequestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{ //nolint:errcheck
			"access_token": fmt.Sprintf("eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.tok%d", tokenRequestCount.Load()),
			"token_type":   "Bearer",
			"expires_in":   3600, // 1 hour
			"scope":        "read:orders write:orders",
		})
	}))
	defer tokenServer.Close()

	// ---------------------------------------------------------------------------
	// Protected API: verifies the Authorization header.
	// ---------------------------------------------------------------------------
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" || len(auth) < 8 || auth[:7] != "Bearer " {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintln(w, `{"error":"missing or malformed Authorization header"}`)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"path":%q,"authorized":true,"token_prefix":%q}`,
			r.URL.Path, auth[7:min(len(auth), 20)])
	}))
	defer apiServer.Close()

	// ---------------------------------------------------------------------------
	// relayoauth.Config - all fields documented.
	// ---------------------------------------------------------------------------
	oauthCfg := relayoauth.Config{
		// The token endpoint that implements RFC 6749 Client Credentials.
		TokenURL: tokenServer.URL + "/oauth/token",

		// OAuth 2.0 client identifier registered with the authorization server.
		ClientID: "my-client-id",

		// Client secret - in production, load from a secret manager or env var.
		ClientSecret: "s3cr3t",

		// Scopes requested from the authorization server.
		Scopes: []string{"read:orders", "write:orders"},

		// Refresh the token this far before its actual expiry to avoid using
		// an expired token due to clock skew. Defaults to 30 s when zero.
		ExpiryDelta: 30 * time.Second,
	}

	// relayoauth.WithClientCredentials wraps the transport with a round-tripper
	// that injects a fresh Bearer token into every outgoing request.
	client := relay.New(
		relay.WithBaseURL(apiServer.URL),
		relayoauth.WithClientCredentials(oauthCfg),
		relay.WithTimeout(10*time.Second),
		relay.WithDefaultHeaders(map[string]string{
			"Accept": "application/json",
		}),
	)

	// ---------------------------------------------------------------------------
	// Make several authenticated requests. The token is fetched once and
	// cached; subsequent requests reuse the same token until ExpiryDelta before
	// its expiry, at which point the client automatically fetches a new one.
	// ---------------------------------------------------------------------------
	endpoints := []string{"/orders", "/orders/123", "/inventory"}

	for _, path := range endpoints {
		resp, err := client.Execute(client.Get(path))
		if err != nil {
			log.Fatalf("GET %s failed: %v", path, err)
		}
		if !resp.IsSuccess() {
			log.Fatalf("GET %s → unexpected status %d: %s", path, resp.StatusCode, resp.String())
		}
		fmt.Printf("GET %-15s → %d: %s\n", path, resp.StatusCode, resp.String())
	}

	fmt.Printf("\nToken requests to authorization server: %d (expected 1 - cached)\n",
		tokenRequestCount.Load())

	// ---------------------------------------------------------------------------
	// POST with JSON body - token injection works for all HTTP methods.
	// ---------------------------------------------------------------------------
	type OrderRequest struct {
		Item     string `json:"item"`
		Quantity int    `json:"quantity"`
	}

	resp, err := client.Execute(
		client.Post("/orders").
			WithJSON(OrderRequest{Item: "widget", Quantity: 42}),
	)
	if err != nil {
		log.Fatalf("POST /orders failed: %v", err)
	}
	fmt.Printf("\nPOST /orders → %d: %s\n", resp.StatusCode, resp.String())
	fmt.Printf("Token requests after POST: %d (still cached)\n", tokenRequestCount.Load())
}

// min is provided for Go 1.21 compatibility.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
