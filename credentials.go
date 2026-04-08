package relay

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// CredentialProvider supplies fresh credentials before each request.
// Implementations may fetch tokens from a vault, refresh OAuth tokens, etc.
type CredentialProvider interface {
	Credentials(ctx context.Context) (Credentials, error)
}

// Credentials holds values to apply to an outgoing request.
type Credentials struct {
	BearerToken string            // sets Authorization: Bearer <token>
	BasicAuth   *BasicAuthCreds   // sets HTTP basic auth
	Headers     map[string]string // arbitrary headers (e.g. X-API-Key)
}

// BasicAuthCreds holds username/password for HTTP basic auth.
type BasicAuthCreds struct {
	Username string
	Password string
}

// applyTo applies the credentials to the given HTTP request.
func (c Credentials) applyTo(r *http.Request) {
	if c.BearerToken != "" {
		r.Header.Set("Authorization", "Bearer "+sanitizeHeaderValue(c.BearerToken))
	} else if c.BasicAuth != nil {
		r.SetBasicAuth(c.BasicAuth.Username, c.BasicAuth.Password)
	}
	for k, v := range c.Headers {
		r.Header.Set(k, sanitizeHeaderValue(v))
	}
}

// staticCredentialProvider always returns the same credentials.
type staticCredentialProvider struct {
	creds Credentials
}

func (s *staticCredentialProvider) Credentials(_ context.Context) (Credentials, error) {
	return s.creds, nil
}

// StaticCredentialProvider always returns the same credentials.
func StaticCredentialProvider(creds Credentials) CredentialProvider {
	return &staticCredentialProvider{creds: creds}
}

// RotatingTokenProvider caches a bearer token and refreshes it when within
// threshold of expiry. Concurrent callers block only during the refresh.
type RotatingTokenProvider struct {
	mu        sync.Mutex
	token     string
	expiry    time.Time
	threshold time.Duration
	refresh   func(ctx context.Context) (token string, expiry time.Time, err error)
}

// NewRotatingTokenProvider returns a RotatingTokenProvider that calls refresh
// to obtain a bearer token and caches it until it is within threshold of
// expiry. The first call always triggers a refresh.
func NewRotatingTokenProvider(
	refresh func(ctx context.Context) (token string, expiry time.Time, err error),
	threshold time.Duration,
) *RotatingTokenProvider {
	return &RotatingTokenProvider{
		refresh:   refresh,
		threshold: threshold,
	}
}

// Credentials returns the cached token, refreshing it first if necessary.
func (r *RotatingTokenProvider) Credentials(ctx context.Context) (Credentials, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.token == "" || time.Until(r.expiry) <= r.threshold {
		tok, exp, err := r.refresh(ctx)
		if err != nil {
			return Credentials{}, err
		}
		r.token = tok
		r.expiry = exp
	}

	return Credentials{BearerToken: r.token}, nil
}

// WithCredentialProvider sets a CredentialProvider that is called before each
// request attempt (including retries). It is applied after default headers and
// the idempotency key, at the same point as [WithSigner].
//
// When both a CredentialProvider and a [RequestSigner] are configured, the
// provider runs first, then the signer.
func WithCredentialProvider(p CredentialProvider) Option {
	return func(c *Config) {
		c.CredentialProvider = p
	}
}
