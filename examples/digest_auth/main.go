// Package main demonstrates relay's HTTP Digest Authentication support.
// Digest auth (RFC 7616) is a challenge-response mechanism - the server sends
// a 401 with a WWW-Authenticate: Digest header, and the client computes an
// HMAC response using the credentials and the nonce, then retries.
//
// relay handles the entire challenge/response cycle automatically when
// WithDigestAuth is configured; callers never see the 401.
package main

import (
	"crypto/md5" //nolint:gosec - MD5 is required by the Digest Auth RFC
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	relay "github.com/jhonsferg/relay"
)

// digestServer returns a handler that requires HTTP Digest Authentication.
// It uses the MD5 algorithm with a random nonce per realm, matching RFC 7616.
func digestServer(realm, username, password string) http.Handler {
	nonce := fmt.Sprintf("%x", rand.New(rand.NewSource(time.Now().UnixNano())).Int63()) //nolint:gosec

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")

		if authHeader == "" || !strings.HasPrefix(authHeader, "Digest ") {
			// No credentials yet - issue the challenge.
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(
				`Digest realm=%q, nonce=%q, algorithm=MD5, qop="auth"`,
				realm, nonce,
			))
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, "authentication required")
			return
		}

		// Parse the Digest response fields.
		fields := parseDigestFields(authHeader[7:]) // strip "Digest "

		ha1 := md5hex(username + ":" + realm + ":" + password) //nolint:gosec
		ha2 := md5hex(r.Method + ":" + fields["uri"])          //nolint:gosec
		expected := md5hex(ha1 + ":" + fields["nonce"] + ":" + //nolint:gosec
			fields["nc"] + ":" + fields["cnonce"] + ":" + fields["qop"] + ":" + ha2)

		if fields["response"] != expected || fields["username"] != username {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, "invalid credentials")
			return
		}

		// Authenticated!
		path := r.URL.Path
		fmt.Fprintf(w, `{"authenticated":true,"user":%q,"path":%q}`, username, path)
	})
}

func md5hex(s string) string {
	h := md5.Sum([]byte(s)) //nolint:gosec
	return fmt.Sprintf("%x", h)
}

// parseDigestFields extracts key=value pairs from a Digest auth header value.
func parseDigestFields(header string) map[string]string {
	fields := make(map[string]string)
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		key, val, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		fields[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(val), `"`)
	}
	return fields
}

func main() {
	const (
		realm    = "api.example.com"
		username = "alice"
		password = "s3cr3t"
	)

	// -------------------------------------------------------------------------
	// 1. Server that requires Digest Authentication.
	// -------------------------------------------------------------------------
	srv := httptest.NewServer(digestServer(realm, username, password))
	defer srv.Close()

	// -------------------------------------------------------------------------
	// 2. Client without auth - receives 401.
	// -------------------------------------------------------------------------
	fmt.Println("=== Without auth → 401 Unauthorized ===")
	bare := relay.New(relay.WithBaseURL(srv.URL), relay.WithDisableRetry())
	resp, err := bare.Execute(bare.Get("/api/resource"))
	if err != nil {
		log.Fatalf("unexpected transport error: %v", err)
	}
	fmt.Printf("  status: %d %s\n  body:   %s\n\n", resp.StatusCode, resp.Status, resp.String())

	// -------------------------------------------------------------------------
	// 3. Client with correct Digest credentials - relay handles the
	//    challenge/response cycle transparently.
	// -------------------------------------------------------------------------
	fmt.Println("=== With correct Digest credentials → 200 OK ===")
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDigestAuth(username, password),
	)

	resp, err = client.Execute(client.Get("/api/resource"))
	if err != nil {
		log.Fatalf("unexpected error: %v", err)
	}
	fmt.Printf("  status: %d %s\n  body:   %s\n\n", resp.StatusCode, resp.Status, resp.String())

	// -------------------------------------------------------------------------
	// 4. Multiple requests - the client reuses the auth state.
	// -------------------------------------------------------------------------
	fmt.Println("=== Multiple authenticated requests ===")
	paths := []string{"/api/users", "/api/orders", "/api/products"}
	for _, path := range paths {
		r, err := client.Execute(client.Get(path))
		if err != nil {
			log.Fatalf("%s failed: %v", path, err)
		}
		fmt.Printf("  GET %-16s → %d  body: %s\n", path, r.StatusCode, r.String())
	}

	// -------------------------------------------------------------------------
	// 5. Wrong credentials → 403 Forbidden.
	// -------------------------------------------------------------------------
	fmt.Println("\n=== Wrong password → 403 Forbidden ===")
	badClient := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDigestAuth(username, "wrong-password"),
		relay.WithDisableRetry(),
	)
	resp, err = badClient.Execute(badClient.Get("/api/secret"))
	if err != nil {
		log.Fatalf("unexpected transport error: %v", err)
	}
	fmt.Printf("  status: %d %s\n  body:   %s\n\n", resp.StatusCode, resp.Status, resp.String())

	// -------------------------------------------------------------------------
	// 6. Combining Digest Auth with other relay features.
	// -------------------------------------------------------------------------
	fmt.Println("=== Digest Auth + retry + logging ===")
	advancedClient := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDigestAuth(username, password),
		relay.WithLogger(relay.NewDefaultLogger(0)), // structured slog output
		relay.WithRetry(&relay.RetryConfig{
			MaxAttempts: 2,
		}),
	)
	resp, err = advancedClient.Execute(advancedClient.Get("/api/profile"))
	if err != nil {
		log.Fatalf("unexpected error: %v", err)
	}
	fmt.Printf("  status: %d  body: %s\n", resp.StatusCode, resp.String())
}
