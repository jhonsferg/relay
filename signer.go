package relay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"
)

// RequestSigner signs outgoing HTTP requests. Implementations may add headers
// (e.g. Authorization, X-Signature), compute HMAC digests, or perform any
// mutation required by the target service's authentication scheme.
//
// Sign is called once per attempt - including retries - after all default
// headers and the idempotency key have been applied, and immediately before
// the request is handed to the underlying transport.
//
// Returning a non-nil error from Sign aborts the attempt and propagates as
// the [Client.Execute] error wrapped in "request signer: ...".
//
// Built-in implementations: ext/sigv4 (AWS Signature Version 4).
// Third-party schemes (OAuth 1.0a, HMAC-SHA256, JWS) can be plugged in by
// implementing this interface and passing the value to [WithSigner].
type RequestSigner interface {
	Sign(req *http.Request) error
}

// RequestSignerFunc is a function that implements [RequestSigner].
// It lets closures be used directly without defining a named type.
//
//	client := relay.New(
//	    relay.WithSigner(relay.RequestSignerFunc(func(r *http.Request) error {
//	        r.Header.Set("X-Api-Key", secret)
//	        return nil
//	    })),
//	)
type RequestSignerFunc func(req *http.Request) error

// Sign implements [RequestSigner].
func (f RequestSignerFunc) Sign(req *http.Request) error { return f(req) }

// WithSigner sets a [RequestSigner] that is invoked for every outgoing
// request. The signer receives the fully-built *http.Request and may add or
// modify headers, compute a body digest, or perform any operation required by
// the target service's authentication scheme.
//
// WithSigner is applied once per attempt (including retries). If the body must
// be re-read on retries, use a body that supports [io.Seeker] or set the body
// via [Request.WithBody] so relay can replay it.
//
// Example - static API key header:
//
//	client := relay.New(
//	    relay.WithSigner(relay.RequestSignerFunc(func(r *http.Request) error {
//	        r.Header.Set("Authorization", "Bearer "+apiKey)
//	        return nil
//	    })),
//	)
//
// Example - HMAC-SHA256 request signing:
//
//	type HMACSigner struct{ secret []byte }
//
//	func (s *HMACSigner) Sign(r *http.Request) error {
//	    mac := hmac.New(sha256.New, s.secret)
//	    fmt.Fprintf(mac, "%s\n%s\n%s", r.Method, r.URL.Path, r.Header.Get("Date"))
//	    r.Header.Set("X-Signature", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
//	    return nil
//	}
//
//	client := relay.New(relay.WithSigner(&HMACSigner{secret: key}))
func WithSigner(s RequestSigner) Option {
	return func(c *Config) { c.Signer = s }
}

// WithRequestSigner is an alias for [WithSigner].
func WithRequestSigner(s RequestSigner) Option { return WithSigner(s) }

// MultiSigner chains multiple [RequestSigner] implementations, applying each
// in order. The first error encountered stops the chain and is returned.
// MultiSigner itself implements [RequestSigner] and is safe for concurrent use
// if all constituent signers are safe for concurrent use.
type MultiSigner struct {
	signers []RequestSigner
}

// NewMultiSigner creates a [MultiSigner] that applies each signer in the order
// provided. Nil entries are silently skipped.
func NewMultiSigner(signers ...RequestSigner) *MultiSigner {
	filtered := make([]RequestSigner, 0, len(signers))
	for _, s := range signers {
		if s != nil {
			filtered = append(filtered, s)
		}
	}
	return &MultiSigner{signers: filtered}
}

// Sign applies each signer in order. It stops and returns an error as soon as
// any signer fails.
func (m *MultiSigner) Sign(req *http.Request) error {
	for _, s := range m.signers {
		if err := s.Sign(req); err != nil {
			return err
		}
	}
	return nil
}

// HMACRequestSigner signs requests with HMAC-SHA256 over a canonical string
// composed of the request method, URL, and a UTC timestamp. It sets two headers:
//   - X-Timestamp (RFC3339 UTC) — replay-protection timestamp.
//   - X-Signature (hex-encoded HMAC-SHA256) — computed over "METHOD\nURL\nTIMESTAMP".
//
// The signing key must be kept secret. HMACRequestSigner is safe for
// concurrent use.
type HMACRequestSigner struct {
	// Key is the HMAC-SHA256 signing key. Must not be empty.
	Key []byte
	// Header is the name of the signature header.
	// Defaults to "X-Signature" when empty.
	Header string
}

// Sign computes and sets the X-Timestamp and signature headers on req.
func (h *HMACRequestSigner) Sign(req *http.Request) error {
	if len(h.Key) == 0 {
		return fmt.Errorf("relay: HMACRequestSigner: key must not be empty")
	}
	ts := time.Now().UTC().Format(time.RFC3339)
	req.Header.Set("X-Timestamp", ts)

	canonical := req.Method + "\n" + req.URL.String() + "\n" + ts
	mac := hmac.New(sha256.New, h.Key)
	_, _ = mac.Write([]byte(canonical))
	sig := hex.EncodeToString(mac.Sum(nil))

	header := h.Header
	if header == "" {
		header = "X-Signature"
	}
	req.Header.Set(header, sig)
	return nil
}
