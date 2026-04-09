// Package oidc provides OIDC/JWT bearer token injection for the relay HTTP
// client. It decouples token acquisition from any specific OIDC library via the
// [TokenSource] interface, and ships three ready-made implementations:
// [StaticToken], [RefreshingTokenSource], and [OAuthTokenSource].
//
// # Basic usage with a fixed token
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    oidc.WithBearerToken(oidc.StaticToken("my-api-key")),
//	)
//
// # Client credentials (auto-refresh)
//
//	src := oidc.RefreshingTokenSource(
//	    "client-id", "client-secret",
//	    "https://auth.example.com/token",
//	)
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    oidc.WithBearerToken(src),
//	)
//
// # Adapting any oauth2.TokenSource
//
//	src := oidc.OAuthTokenSource(myOAuth2TokenSource)
//	client := relay.New(oidc.WithBearerToken(src))
package oidc

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/jhonsferg/relay"
)

// TokenSource is the single abstraction this package requires: given a context,
// return a raw bearer token string. Implementations are free to cache, refresh,
// or derive the token in any way.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// staticToken is a [TokenSource] that always returns the same token string.
type staticToken struct{ token string }

// StaticToken returns a [TokenSource] that always returns the given token
// unchanged. Useful for API keys, service-account tokens, and tests.
func StaticToken(token string) TokenSource { return &staticToken{token: token} }

func (s *staticToken) Token(_ context.Context) (string, error) { return s.token, nil }

// WithBearerToken returns a [relay.Option] that injects an Authorization header
// before every request. The header value is "Bearer <token>" where the token is
// obtained by calling src.Token with the request context. If src.Token returns
// an error the request is aborted with that error.
func WithBearerToken(src TokenSource) relay.Option {
	return relay.WithOnBeforeRequest(func(ctx context.Context, req *relay.Request) error {
		tok, err := src.Token(ctx)
		if err != nil {
			return fmt.Errorf("oidc: fetch bearer token: %w", err)
		}
		req.WithHeader("Authorization", "Bearer "+tok)
		return nil
	})
}

// oauthAdapter wraps an [oauth2.TokenSource] to satisfy [TokenSource].
type oauthAdapter struct{ ts oauth2.TokenSource }

// OAuthTokenSource adapts any [oauth2.TokenSource] to relay's [TokenSource]
// interface. This lets callers plug in any token source from the
// golang.org/x/oauth2 ecosystem (PKCE, device flow, JWT assertion, etc.).
func OAuthTokenSource(ts oauth2.TokenSource) TokenSource { return &oauthAdapter{ts: ts} }

func (a *oauthAdapter) Token(_ context.Context) (string, error) {
	tok, err := a.ts.Token()
	if err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

// RefreshingTokenSource returns a [TokenSource] that uses the OAuth 2.0 client
// credentials grant to fetch and auto-refresh access tokens. Tokens are cached
// until they expire; refresh happens transparently on the next call.
//
// Deprecated: Use [RefreshingTokenSourceContext] to pass a context so that
// token refresh requests can be cancelled or time-bounded.
func RefreshingTokenSource(clientID, clientSecret, tokenURL string) TokenSource {
	return RefreshingTokenSourceContext(context.Background(), clientID, clientSecret, tokenURL)
}

// RefreshingTokenSourceContext is like [RefreshingTokenSource] but accepts a
// context that governs the lifetime of token refresh HTTP requests. Pass the
// application's root context (or a context with a deadline) so that refresh
// operations honour cancellation and shutdown signals.
func RefreshingTokenSourceContext(ctx context.Context, clientID, clientSecret, tokenURL string) TokenSource {
	cfg := &clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     tokenURL,
	}
	ts := cfg.TokenSource(ctx)
	return OAuthTokenSource(ts)
}
