package relay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

// RequestSigner signs an outgoing HTTP request in-place.
// Sign is called once per attempt — including retries — after all default
// headers and the idempotency key have been applied, and immediately before
// the request is handed to the underlying transport.
//
// Returning a non-nil error from Sign aborts the attempt and propagates as
// the [Client.Execute] error wrapped in "request signer: ...".
//
// Implementations must be safe for concurrent use.
//
// Built-in implementations: [HMACRequestSigner], ext/sigv4 (AWS SigV4).
// Third-party schemes (OAuth 1.0a, RSA, JWS) can be plugged in by
// implementing this interface and passing the value to [WithRequestSigner].
type RequestSigner interface {
	Sign(ctx context.Context, req *http.Request) error
}

// SignerFunc is an adapter allowing plain functions to be used as
// [RequestSigner]s without defining a named type.
//
//	relay.New(
//	    relay.WithRequestSigner(relay.SignerFunc(func(ctx context.Context, r *http.Request) error {
//	        r.Header.Set("X-Api-Key", secret)
//	        return nil
//	    })),
//	)
type SignerFunc func(ctx context.Context, req *http.Request) error

// Sign implements [RequestSigner].
func (f SignerFunc) Sign(ctx context.Context, req *http.Request) error { return f(ctx, req) }

// RequestSignerFunc is an alias for [SignerFunc] retained for backwards
// compatibility. New code should prefer [SignerFunc].
type RequestSignerFunc = SignerFunc

// WithRequestSigner attaches a signer that is called on every request just
// before it is sent. Multiple signers may be chained with [NewMultiSigner].
//
// WithRequestSigner is applied once per attempt (including retries). If the
// body must be re-read on retries, use a body that supports [io.Seeker] or
// set the body via [Request.WithBody] so relay can replay it.
func WithRequestSigner(s RequestSigner) Option {
	return func(c *Config) { c.Signer = s }
}

// WithSigner is an alias for [WithRequestSigner] retained for backwards
// compatibility. New code should prefer [WithRequestSigner].
func WithSigner(s RequestSigner) Option { return WithRequestSigner(s) }

// MultiSigner chains multiple [RequestSigner]s; each is applied in order.
// The first signer to return a non-nil error stops the chain.
type MultiSigner struct {
	signers []RequestSigner
}

// NewMultiSigner returns a [MultiSigner] that applies each signer in order.
func NewMultiSigner(signers ...RequestSigner) *MultiSigner {
	return &MultiSigner{signers: signers}
}

// Sign applies each signer in order. It stops and returns the first error.
func (m *MultiSigner) Sign(ctx context.Context, req *http.Request) error {
	for _, s := range m.signers {
		if err := s.Sign(ctx, req); err != nil {
			return err
		}
	}
	return nil
}

// HMACRequestSigner signs requests with HMAC-SHA256 over a canonical string
// of the form "METHOD\nURL\nTIMESTAMP". It sets the "X-Timestamp" header to
// the current UTC time (RFC 3339) and the signature header (default
// "X-Signature") to the lower-case hex-encoded HMAC-SHA256 digest.
//
// HMACRequestSigner is safe for concurrent use.
type HMACRequestSigner struct {
	// Key is the HMAC secret used to sign the canonical string.
	Key []byte
	// Header is the name of the request header that receives the hex-encoded
	// HMAC-SHA256 signature. Defaults to "X-Signature" when empty.
	Header string
}

// Sign implements [RequestSigner].
func (h *HMACRequestSigner) Sign(_ context.Context, req *http.Request) error {
	ts := time.Now().UTC().Format(time.RFC3339)
	req.Header.Set("X-Timestamp", ts)

	canonical := fmt.Sprintf("%s\n%s\n%s", req.Method, req.URL.String(), ts)
	mac := hmac.New(sha256.New, h.Key)
	mac.Write([]byte(canonical)) //nolint:errcheck // hash.Hash.Write never returns an error

	hdr := h.Header
	if hdr == "" {
		hdr = "X-Signature"
	}
	req.Header.Set(hdr, hex.EncodeToString(mac.Sum(nil)))
	return nil
}
