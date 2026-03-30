package relay

import (
	"bytes"
	"errors"
	"net/url"
)

// newBytesReader returns an *io.Reader backed by a copy of b.
func newBytesReader(b []byte) *bytes.Reader {
	return bytes.NewReader(b)
}

// isRedirectError reports whether err was produced by the CheckRedirect policy
// stopping redirect following. Such errors should not be counted as circuit
// breaker failures because they reflect client-side policy, not downstream
// unavailability.
func isRedirectError(err error) bool {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// http.Client wraps redirect stops in a *url.Error. The underlying Err
		// is NOT a net.Error (no Timeout/Temporary), which distinguishes it from
		// real transport failures.
		var netErr interface {
			Timeout() bool
			Temporary() bool
		}
		return !errors.As(urlErr.Err, &netErr)
	}
	return false
}
