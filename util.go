package relay

import (
	"bytes"
	"errors"
)

// newBytesReader returns an *io.Reader backed by a copy of b.
func newBytesReader(b []byte) *bytes.Reader {
	return bytes.NewReader(b)
}

// errMaxRedirectsExceeded is wrapped into the error redirectPolicy (client.go)
// returns when cfg.MaxRedirects is reached, so isRedirectError can identify
// it precisely via errors.Is instead of guessing from unrelated error traits.
var errMaxRedirectsExceeded = errors.New("redirect limit exceeded")

// isRedirectError reports whether err was produced by the CheckRedirect
// policy's own MaxRedirects limit (not a BeforeRedirectHooks-returned error,
// which may itself reflect a real downstream problem the hook detected).
// Such errors should not be counted as circuit breaker failures because they
// reflect client-side policy, not downstream unavailability.
//
// A previous version of this check used a heuristic (http.Client wraps
// redirect stops in a *url.Error whose underlying Err is "not a net.Error" -
// no Timeout()/Temporary() methods) instead of identifying the specific
// error. That heuristic is far broader than "is a MaxRedirects stop": any
// non-net.Error transport failure reaching this point - e.g. a TLS
// certificate error (x509.CertificateInvalidError, x509.HostnameError,
// x509.UnknownAuthorityError implement neither method) - was misclassified
// as a policy error and its circuit-breaker failure silently dropped.
func isRedirectError(err error) bool {
	return errors.Is(err, errMaxRedirectsExceeded)
}
