package relay

import (
	"crypto/md5" //nolint:gosec
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"net/http"
	"strings"
)

// digestTransport implements HTTP Digest Authentication (RFC 7616) as a
// RoundTripper. On a 401 with WWW-Authenticate: Digest it calculates the
// Authorization header and retries once.
type digestTransport struct {
	base     http.RoundTripper
	username string
	password string
}

func newDigestTransport(base http.RoundTripper, username, password string) http.RoundTripper {
	return &digestTransport{base: base, username: username, password: password}
}

// RoundTrip sends the request. If the server returns 401 with Digest challenge,
// it retries with the computed Authorization header.
func (t *digestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// First attempt without auth.
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	challenge := resp.Header.Get("WWW-Authenticate")
	if !strings.HasPrefix(strings.ToLower(challenge), "digest ") {
		return resp, nil
	}

	// Must drain and close the 401 body before retrying.
	_ = resp.Body.Close() //nolint:errcheck

	params := parseDigestChallenge(challenge[7:])

	authHeader, err := computeDigestAuth(t.username, t.password, req.Method, req.URL.RequestURI(), params)
	if err != nil {
		return nil, fmt.Errorf("relay: digest auth: %w", err)
	}

	// Clone request for the retry.
	retryReq := req.Clone(req.Context())
	retryReq.Header.Set("Authorization", authHeader)

	return t.base.RoundTrip(retryReq)
}

// parseDigestChallenge parses the parameter list from a Digest challenge string.
func parseDigestChallenge(s string) map[string]string {
	params := make(map[string]string)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		idx := strings.IndexByte(part, '=')
		if idx < 0 {
			continue
		}
		k := strings.TrimSpace(part[:idx])
		v := strings.TrimSpace(part[idx+1:])
		v = strings.Trim(v, `"`)
		params[k] = v
	}
	return params
}

// computeDigestAuth builds the Authorization header value for Digest auth.
func computeDigestAuth(username, password, method, uri string, params map[string]string) (string, error) {
	realm := params["realm"]
	nonce := params["nonce"]
	opaque := params["opaque"]
	algorithm := params["algorithm"]
	qop := params["qop"]

	if algorithm == "" {
		algorithm = "MD5"
	}

	var h func() hash.Hash
	switch strings.ToUpper(strings.TrimSuffix(algorithm, "-sess")) {
	case "MD5":
		h = func() hash.Hash { return md5.New() } //nolint:gosec
	case "SHA-256":
		h = func() hash.Hash { return sha256.New() }
	default:
		h = func() hash.Hash { return md5.New() } //nolint:gosec
	}

	ha1 := hexHash(h, username+":"+realm+":"+password)
	if strings.HasSuffix(strings.ToUpper(algorithm), "-SESS") {
		cnonce, _ := generateCNonce()
		ha1 = hexHash(h, ha1+":"+nonce+":"+cnonce)
	}

	ha2 := hexHash(h, method+":"+uri)

	var response string
	var cnonce string
	var nc = "00000001"

	if strings.Contains(qop, "auth") {
		var err error
		cnonce, err = generateCNonce()
		if err != nil {
			return "", err
		}
		response = hexHash(h, ha1+":"+nonce+":"+nc+":"+cnonce+":auth:"+ha2)
	} else {
		response = hexHash(h, ha1+":"+nonce+":"+ha2)
	}

	var sb strings.Builder
	sb.WriteString("Digest ")
	sb.WriteString(fmt.Sprintf(`username="%s"`, username))
	sb.WriteString(fmt.Sprintf(`, realm="%s"`, realm))
	sb.WriteString(fmt.Sprintf(`, nonce="%s"`, nonce))
	sb.WriteString(fmt.Sprintf(`, uri="%s"`, uri))
	sb.WriteString(fmt.Sprintf(`, algorithm=%s`, algorithm))
	if strings.Contains(qop, "auth") {
		sb.WriteString(`, qop=auth`)
		sb.WriteString(fmt.Sprintf(`, nc=%s`, nc))
		sb.WriteString(fmt.Sprintf(`, cnonce="%s"`, cnonce))
	}
	sb.WriteString(fmt.Sprintf(`, response="%s"`, response))
	if opaque != "" {
		sb.WriteString(fmt.Sprintf(`, opaque="%s"`, opaque))
	}

	return sb.String(), nil
}

func hexHash(newHash func() hash.Hash, data string) string {
	h := newHash()
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}

func generateCNonce() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
