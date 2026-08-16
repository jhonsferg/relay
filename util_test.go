package relay

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"testing"
)

func TestIsRedirectError_TrueForMaxRedirectsStop(t *testing.T) {
	err := &url.Error{
		Op:  "Get",
		URL: "http://example.com/",
		Err: fmt.Errorf("stopped after %d redirects: %w", 10, errMaxRedirectsExceeded),
	}
	if !isRedirectError(err) {
		t.Error("expected true for a MaxRedirects-stop error")
	}
}

// TestIsRedirectError_FalseForUnrelatedNonNetError guards against the
// previous heuristic ("wrapped in *url.Error and the underlying err is not
// a net.Error") misclassifying any non-net.Error transport failure as a
// redirect-policy stop. A TLS certificate error is a concrete, realistic
// example: x509.CertificateInvalidError implements neither Timeout() nor
// Temporary(), so the old heuristic treated it as a policy error and
// silently skipped recording it as a circuit-breaker failure.
func TestIsRedirectError_FalseForUnrelatedNonNetError(t *testing.T) {
	certErr := x509.CertificateInvalidError{Reason: x509.Expired}
	err := &url.Error{Op: "Get", URL: "https://example.com/", Err: certErr}
	if isRedirectError(err) {
		t.Error("expected false for a TLS certificate error - it must count as a real circuit-breaker failure")
	}
}

func TestIsRedirectError_FalseForPlainError(t *testing.T) {
	if isRedirectError(errors.New("some other error")) {
		t.Error("expected false for an unwrapped, unrelated error")
	}
	if isRedirectError(nil) {
		t.Error("expected false for nil")
	}
}

// TestIsRedirectError_FalseForBeforeRedirectHookError guards the documented
// scope: only the CheckRedirect policy's own MaxRedirects limit is a
// "policy" error; a BeforeRedirectHooks-returned error may itself reflect a
// real downstream problem the hook detected, so it must not be silently
// exempted from circuit-breaker accounting the way a MaxRedirects stop is.
func TestIsRedirectError_FalseForBeforeRedirectHookError(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "http://example.com/", Err: errors.New("hook rejected redirect to untrusted host")}
	if isRedirectError(err) {
		t.Error("expected false for a BeforeRedirectHooks-returned error")
	}
}
