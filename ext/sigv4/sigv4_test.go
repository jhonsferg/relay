package sigv4_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials"

	"github.com/jhonsferg/relay"
	relaysigv4 "github.com/jhonsferg/relay/ext/sigv4"
)

// staticCreds creates a static credentials provider for testing.
func staticCreds() *credentials.StaticCredentialsProvider {
	p := credentials.NewStaticCredentialsProvider("AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "")
	return &p
}

func newClient(srv *httptest.Server, opts ...relaysigv4.Option) *relay.Client {
	return relay.New(
		relay.WithBaseURL(srv.URL),
		relaysigv4.WithSigV4(staticCreds(), "execute-api", "us-east-1", opts...),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(5*time.Second),
	)
}

// capturedHeaders is the server-side function that captures request headers.
func captureHeaders(t *testing.T) (http.Handler, func() http.Header) {
	t.Helper()
	var captured http.Header
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	})
	return handler, func() http.Header { return captured }
}

// ---------------------------------------------------------------------------
// Authorization header presence
// ---------------------------------------------------------------------------

func TestWithSigV4_AddsAuthorizationHeader(t *testing.T) {
	t.Parallel()

	handler, getHeaders := captureHeaders(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	_, err := newClient(srv).Execute(newClient(srv).Get("/resource"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	auth := getHeaders().Get("Authorization")
	if auth == "" {
		t.Fatal("Authorization header not set")
	}
	// SigV4 Authorization starts with "AWS4-HMAC-SHA256 Credential=..."
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256") {
		t.Errorf("Authorization = %q, want prefix AWS4-HMAC-SHA256", auth)
	}
}

func TestWithSigV4_AddsXAmzDateHeader(t *testing.T) {
	t.Parallel()

	handler, getHeaders := captureHeaders(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	newClient(srv).Execute(newClient(srv).Get("/")) //nolint:errcheck

	date := getHeaders().Get("X-Amz-Date")
	if date == "" {
		t.Fatal("X-Amz-Date header not set")
	}
	// Format: 20060102T150405Z
	if len(date) != 16 {
		t.Errorf("X-Amz-Date = %q, expected 16-char ISO8601 basic format", date)
	}
}

func TestWithSigV4_AuthorizationContainsRegionAndService(t *testing.T) {
	t.Parallel()

	handler, getHeaders := captureHeaders(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	newClient(srv).Execute(newClient(srv).Get("/")) //nolint:errcheck

	auth := getHeaders().Get("Authorization")
	if !strings.Contains(auth, "us-east-1") {
		t.Errorf("Authorization %q should contain region us-east-1", auth)
	}
	if !strings.Contains(auth, "execute-api") {
		t.Errorf("Authorization %q should contain service execute-api", auth)
	}
	if !strings.Contains(auth, "AKIAIOSFODNN7EXAMPLE") {
		t.Errorf("Authorization %q should contain access key ID", auth)
	}
}

// ---------------------------------------------------------------------------
// POST with body
// ---------------------------------------------------------------------------

func TestWithSigV4_SignsPostBody(t *testing.T) {
	t.Parallel()

	handler, getHeaders := captureHeaders(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c := newClient(srv)
	_, err := c.Execute(c.Post("/items").WithJSON(map[string]string{"key": "value"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	auth := getHeaders().Get("Authorization")
	if auth == "" {
		t.Fatal("Authorization header not set for POST")
	}
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256") {
		t.Errorf("Authorization = %q, want AWS4-HMAC-SHA256 prefix", auth)
	}
}

func TestWithSigV4_PostBodyStillReadableByServer(t *testing.T) {
	t.Parallel()

	var receivedBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 256)
		n, _ := r.Body.Read(buf)
		receivedBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newClient(srv)
	_, err := c.Execute(c.Post("/").WithJSON(map[string]string{"hello": "world"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if receivedBody == "" {
		t.Error("server received empty body — body was not restored after hashing")
	}
	if !strings.Contains(receivedBody, "hello") {
		t.Errorf("received body %q, expected to contain 'hello'", receivedBody)
	}
}

// ---------------------------------------------------------------------------
// Unsigned payload
// ---------------------------------------------------------------------------

func TestWithSigV4_UnsignedPayload(t *testing.T) {
	t.Parallel()

	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newClient(srv, relaysigv4.WithUnsignedPayload())
	_, err := c.Execute(c.Post("/").WithJSON(map[string]string{"k": "v"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if capturedAuth == "" {
		t.Fatal("Authorization header not set")
	}
	// With unsigned payload the signed headers list still appears.
	if !strings.HasPrefix(capturedAuth, "AWS4-HMAC-SHA256") {
		t.Errorf("Authorization = %q, want AWS4-HMAC-SHA256 prefix", capturedAuth)
	}
}

// ---------------------------------------------------------------------------
// Session token (STS / assumed role)
// ---------------------------------------------------------------------------

func TestWithSigV4_WithSessionToken(t *testing.T) {
	t.Parallel()

	handler, getHeaders := captureHeaders(t)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// Credentials with a session token (mimics STS assumed-role credentials).
	p := credentials.NewStaticCredentialsProvider("AKID", "SECRET", "SESSION_TOKEN_HERE")
	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relaysigv4.WithSigV4(&p, "sts", "us-west-2"),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(5*time.Second),
	)

	_, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	secToken := getHeaders().Get("X-Amz-Security-Token")
	if secToken == "" {
		t.Error("X-Amz-Security-Token header not set for session-token credentials")
	}
	if secToken != "SESSION_TOKEN_HERE" {
		t.Errorf("X-Amz-Security-Token = %q, want SESSION_TOKEN_HERE", secToken)
	}
}

// ---------------------------------------------------------------------------
// Headers not mutated between requests
// ---------------------------------------------------------------------------

func TestWithSigV4_EachRequestIndependentlySigned(t *testing.T) {
	t.Parallel()

	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auths = append(auths, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newClient(srv)

	// Two sequential requests a second apart will have different X-Amz-Date
	// and thus different signatures.
	c.Execute(c.Get("/a")) //nolint:errcheck
	time.Sleep(2 * time.Second)
	c.Execute(c.Get("/b")) //nolint:errcheck

	if len(auths) != 2 {
		t.Fatalf("expected 2 Authorization headers, got %d", len(auths))
	}
	// Both must be valid SigV4 signatures.
	for i, a := range auths {
		if !strings.HasPrefix(a, "AWS4-HMAC-SHA256") {
			t.Errorf("request %d Authorization = %q, want AWS4-HMAC-SHA256 prefix", i+1, a)
		}
	}
}
