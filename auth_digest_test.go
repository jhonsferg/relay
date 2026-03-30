package relay

import (
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
	defer resp.Body.Close()
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
	defer resp.Body.Close()
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
	defer resp.Body.Close()
	// Should not retry for non-Digest challenge, return original 401.
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 passthrough, got %d", resp.StatusCode)
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
