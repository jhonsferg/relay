// Package oauth provides OAuth 2.0 Client Credentials support for the relay
// HTTP client. Import this package and call [WithClientCredentials] to add
// automatic Bearer token injection to any relay client.
//
// Usage:
//
//	import (
//	    "github.com/jhonsferg/relay"
//	    relaymauth "github.com/jhonsferg/relay/ext/oauth"
//	)
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relayoauth.WithClientCredentials(relayoauth.Config{
//	        TokenURL:     "https://auth.example.com/token",
//	        ClientID:     "my-app",
//	        ClientSecret: "s3cr3t",
//	        Scopes:       []string{"read", "write"},
//	    }),
//	)
package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jhonsferg/relay"
)

// Config holds OAuth 2.0 Client Credentials flow settings.
type Config struct {
	// TokenURL is the endpoint that issues access tokens.
	TokenURL string

	// ClientID is the registered application identifier.
	ClientID string

	// ClientSecret is the application secret.
	ClientSecret string

	// Scopes is the list of OAuth 2.0 scopes to request.
	Scopes []string

	// ExpiryDelta is how far before the token's actual expiry to proactively
	// refresh. Defaults to 30 seconds.
	ExpiryDelta time.Duration

	// ExtraParams are additional form parameters sent in the token request.
	ExtraParams map[string]string
}

// WithClientCredentials returns a [relay.Option] that enables automatic Bearer
// token injection via the OAuth 2.0 Client Credentials flow. Tokens are
// fetched lazily on the first request and refreshed automatically before
// expiry.
func WithClientCredentials(cfg Config) relay.Option {
	if cfg.ExpiryDelta == 0 {
		cfg.ExpiryDelta = 30 * time.Second
	}
	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		src := &tokenSource{
			cfg:        cfg,
			httpClient: &http.Client{Transport: &http.Transport{}},
		}
		return &roundTripper{base: next, source: src}
	})
}

// token is a cached OAuth 2.0 access token with its expiry.
type token struct {
	accessToken string
	expiresAt   time.Time
}

// tokenSource manages a cached, auto-refreshing OAuth 2.0 token.
type tokenSource struct {
	cfg        Config
	httpClient *http.Client

	mu  sync.RWMutex
	tok *token
}

// get returns a valid access token, fetching or refreshing as needed.
func (s *tokenSource) get(ctx context.Context) (string, error) {
	s.mu.RLock()
	tok := s.tok
	s.mu.RUnlock()

	if tok != nil && time.Now().Add(s.cfg.ExpiryDelta).Before(tok.expiresAt) {
		return tok.accessToken, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock.
	if s.tok != nil && time.Now().Add(s.cfg.ExpiryDelta).Before(s.tok.expiresAt) {
		return s.tok.accessToken, nil
	}

	newTok, err := s.fetch(ctx)
	if err != nil {
		return "", err
	}
	s.tok = newTok
	return newTok.accessToken, nil
}

func (s *tokenSource) fetch(ctx context.Context) (*token, error) {
	params := url.Values{}
	params.Set("grant_type", "client_credentials")
	params.Set("client_id", s.cfg.ClientID)
	params.Set("client_secret", s.cfg.ClientSecret)
	if len(s.cfg.Scopes) > 0 {
		params.Set("scope", strings.Join(s.cfg.Scopes, " "))
	}
	for k, v := range s.cfg.ExtraParams {
		params.Set(k, v)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.TokenURL,
		strings.NewReader(params.Encode()))
	if err != nil {
		return nil, fmt.Errorf("relay/oauth: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("relay/oauth: token fetch: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("relay/oauth: read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay/oauth: token endpoint returned %d: %s", resp.StatusCode, body)
	}

	var tokenResp struct {
		AccessToken string  `json:"access_token"`
		ExpiresIn   float64 `json:"expires_in"`
		Error       string  `json:"error"`
		ErrorDesc   string  `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("relay/oauth: parse token response: %w", err)
	}
	if tokenResp.Error != "" {
		return nil, fmt.Errorf("relay/oauth: error %q: %s", tokenResp.Error, tokenResp.ErrorDesc)
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("relay/oauth: token endpoint returned empty access_token")
	}

	return &token{
		accessToken: tokenResp.AccessToken,
		expiresAt:   time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}, nil
}

// roundTripper injects Bearer tokens into every outgoing request.
type roundTripper struct {
	base   http.RoundTripper
	source *tokenSource
}

func (t *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := t.source.get(req.Context())
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.Header.Set("Authorization", "Bearer "+tok)
	return t.base.RoundTrip(clone)
}
