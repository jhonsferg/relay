package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseDigestChallenge_BasicFields(t *testing.T) {
	t.Parallel()
	input := `realm="testrealm@host.com", nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093", algorithm=MD5, qop="auth"`
	params := parseDigestChallenge(input)
	if params["realm"] != "testrealm@host.com" {
		t.Errorf("realm: got %q", params["realm"])
	}
	if params["nonce"] != "dcd98b7102dd2f0e8b11d0f600bfb0c093" {
		t.Errorf("nonce: got %q", params["nonce"])
	}
	if params["algorithm"] != "MD5" {
		t.Errorf("algorithm: got %q", params["algorithm"])
	}
	if params["qop"] != "auth" {
		t.Errorf("qop: got %q", params["qop"])
	}
}

func TestParseDigestChallenge_NoEquals(t *testing.T) {
	t.Parallel()
	// Parts without '=' should be skipped gracefully.
	params := parseDigestChallenge(`realm="test", badpart, nonce="abc"`)
	if params["realm"] != "test" {
		t.Errorf("realm: got %q", params["realm"])
	}
	if params["nonce"] != "abc" {
		t.Errorf("nonce: got %q", params["nonce"])
	}
}

func TestComputeDigestAuth_MD5_NoQop(t *testing.T) {
	t.Parallel()
	params := map[string]string{
		"realm":     "testrealm@host.com",
		"nonce":     "dcd98b7102dd2f0e8b11d0f600bfb0c093",
		"algorithm": "MD5",
	}
	auth, err := computeDigestAuth("Mufasa", "CircleOfLife", "GET", "/dir/index.html", params)
	if err != nil {
		t.Fatalf("computeDigestAuth error: %v", err)
	}
	if !strings.HasPrefix(auth, "Digest ") {
		t.Errorf("expected 'Digest ' prefix, got %q", auth[:min(20, len(auth))])
	}
	if !strings.Contains(auth, `username="Mufasa"`) {
		t.Errorf("expected username in auth header")
	}
	if !strings.Contains(auth, `realm="testrealm@host.com"`) {
		t.Errorf("expected realm in auth header")
	}
}

func TestComputeDigestAuth_MD5_WithQop(t *testing.T) {
	t.Parallel()
	params := map[string]string{
		"realm":     "example.com",
		"nonce":     "abc123",
		"algorithm": "MD5",
		"qop":       "auth",
	}
	auth, err := computeDigestAuth("user", "pass", "POST", "/api/data", params)
	if err != nil {
		t.Fatalf("computeDigestAuth error: %v", err)
	}
	if !strings.Contains(auth, "qop=auth") {
		t.Errorf("expected qop=auth in header, got %q", auth)
	}
	if !strings.Contains(auth, "nc=00000001") {
		t.Errorf("expected nc=00000001 in header")
	}
	if !strings.Contains(auth, "cnonce=") {
		t.Errorf("expected cnonce in header")
	}
}

func TestComputeDigestAuth_SHA256(t *testing.T) {
	t.Parallel()
	params := map[string]string{
		"realm":     "sha256realm",
		"nonce":     "nonce256",
		"algorithm": "SHA-256",
	}
	auth, err := computeDigestAuth("user", "pass", "GET", "/resource", params)
	if err != nil {
		t.Fatalf("computeDigestAuth error: %v", err)
	}
	if !strings.Contains(auth, "algorithm=SHA-256") {
		t.Errorf("expected algorithm=SHA-256 in header")
	}
}

func TestComputeDigestAuth_WithOpaque(t *testing.T) {
	t.Parallel()
	params := map[string]string{
		"realm":  "realm",
		"nonce":  "nonce1",
		"opaque": "deadbeef",
	}
	auth, err := computeDigestAuth("u", "p", "GET", "/", params)
	if err != nil {
		t.Fatalf("computeDigestAuth error: %v", err)
	}
	if !strings.Contains(auth, `opaque="deadbeef"`) {
		t.Errorf("expected opaque in header, got %q", auth)
	}
}

func TestComputeDigestAuth_DefaultAlgorithm(t *testing.T) {
	t.Parallel()
	// When algorithm is empty it should default to MD5.
	params := map[string]string{
		"realm": "realm",
		"nonce": "nonce1",
	}
	auth, err := computeDigestAuth("u", "p", "GET", "/", params)
	if err != nil {
		t.Fatalf("computeDigestAuth error: %v", err)
	}
	if auth == "" {
		t.Error("expected non-empty auth header")
	}
}

func TestDigestTransport_RoundTrip_NoChallenge(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	transport := newDigestTransport(http.DefaultTransport, "user", "pass")
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestDigestTransport_RoundTrip_DigestChallenge(t *testing.T) {
	t.Parallel()
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="testrealm", nonce="testnonce", algorithm=MD5, qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	transport := newDigestTransport(http.DefaultTransport, "user", "pass")
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/protected", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after digest auth, got %d", resp.StatusCode)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestDigestTransport_RoundTrip_NonDigest401(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", "Basic realm=\"test\"")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	transport := newDigestTransport(http.DefaultTransport, "user", "pass")
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck
	// Should not retry for non-Digest challenge, return original 401.
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 passthrough, got %d", resp.StatusCode)
	}
}

// TestDigestTransport_RoundTrip_POSTWithBody_ReplaysBody guards against a
// regression where the authenticated retry sent an empty body for
// non-GET requests. req.Clone shallow-copies Body (the same reader as the
// unauthenticated first attempt, already drained), so the retry must obtain
// a fresh body via req.GetBody.
//
// The base transport disables keep-alives deliberately: net/http.Transport
// has its own unrelated internal retry-via-GetBody mechanism for a request
// that fails to write on a *reused* idle connection, which would otherwise
// accidentally paper over this exact bug when both attempts happen to reuse
// the same pooled connection, making the test non-deterministic. Forcing a
// fresh connection per attempt isolates the assertion to digestTransport's
// own body-replay logic.
func TestDigestTransport_RoundTrip_POSTWithBody_ReplaysBody(t *testing.T) {
	t.Parallel()
	var gotBodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBodies = append(gotBodies, string(body))
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="testrealm", nonce="testnonce", algorithm=MD5, qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	base := &http.Transport{DisableKeepAlives: true}
	transport := newDigestTransport(base, "user", "pass")
	const payload = `{"name":"Alice"}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/protected", strings.NewReader(payload))
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after digest auth, got %d", resp.StatusCode)
	}

	if len(gotBodies) != 2 {
		t.Fatalf("expected 2 requests to reach the server, got %d", len(gotBodies))
	}
	if gotBodies[0] != payload {
		t.Errorf("first (unauthenticated) attempt body = %q, want %q", gotBodies[0], payload)
	}
	if gotBodies[1] != payload {
		t.Errorf("authenticated retry body = %q, want %q (body was not replayed)", gotBodies[1], payload)
	}
}

// TestDigestTransport_RoundTrip_NonReplayableBody_ReturnsError confirms the
// fix fails loudly (rather than silently sending an empty body) when the
// original request body has no GetBody - e.g. a raw io.Reader that isn't one
// of the few types (*bytes.Reader, *bytes.Buffer, *strings.Reader) for which
// net/http automatically populates GetBody.
func TestDigestTransport_RoundTrip_NonReplayableBody_ReturnsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="testrealm", nonce="testnonce", algorithm=MD5, qop="auth"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	transport := newDigestTransport(http.DefaultTransport, "user", "pass")
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/protected", io.NopCloser(strings.NewReader(`{"x":1}`)))
	req.GetBody = nil // simulate a non-replayable streaming body
	_, err := transport.RoundTrip(req)
	if err == nil {
		t.Fatal("expected an error for a non-replayable body on digest-auth retry, got nil")
	}
}

func TestWithDigestAuth_Option(t *testing.T) {
	t.Parallel()
	c := New(WithDigestAuth("user", "pass"))
	if c == nil {
		t.Fatal("New with WithDigestAuth returned nil")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
