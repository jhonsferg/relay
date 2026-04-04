package relay

import "net/http"

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
