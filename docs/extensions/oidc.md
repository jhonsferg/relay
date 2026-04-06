# OIDC / JWT Bearer Token Extension

The OIDC extension provides automatic JWT bearer token injection for relay clients.
It decouples token acquisition from any specific OIDC library via a small
`TokenSource` interface, and ships three ready-made implementations.

**Import path:** `github.com/jhonsferg/relay/ext/oidc`

---

## Overview

Many APIs protect their endpoints with short-lived JWTs issued by an identity
provider (IdP). Rather than managing token fetch, caching, and refresh in
application code, attach `oidc.WithBearerToken` once and every relay request
receives a valid `Authorization: Bearer <token>` header automatically.

Three token source implementations ship out of the box:

| Function | Description |
|---|---|
| `StaticToken(token)` | Fixed string -- useful for API keys and tests |
| `RefreshingTokenSource(id, secret, url)` | Client credentials grant via `golang.org/x/oauth2` |
| `OAuthTokenSource(ts)` | Adapts any `oauth2.TokenSource` |

---

## Installation

```bash
go get github.com/jhonsferg/relay/ext/oidc@latest
```

---

## Quick Start

### Fixed token (API key or static JWT)

```go
import (
    "github.com/jhonsferg/relay"
    relayoidc "github.com/jhonsferg/relay/ext/oidc"
)

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relayoidc.WithBearerToken(relayoidc.StaticToken("eyJhbGci...")),
)
```

### Auto-refreshing client credentials

```go
src := relayoidc.RefreshingTokenSource(
    "my-client-id",
    "my-client-secret",
    "https://auth.example.com/oauth2/token",
)

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relayoidc.WithBearerToken(src),
)
```

### Any `oauth2.TokenSource`

```go
import "golang.org/x/oauth2"

var ts oauth2.TokenSource = myCustomTokenSource()

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relayoidc.WithBearerToken(relayoidc.OAuthTokenSource(ts)),
)
```

---

## API Reference

### `TokenSource`

```go
type TokenSource interface {
    Token(ctx context.Context) (string, error)
}
```

The single abstraction this package requires. Implement this interface to plug
in any token acquisition strategy -- PKCE, device flow, JWT assertion, etc.

### `StaticToken(token string) TokenSource`

Returns a `TokenSource` that always returns the given string unchanged. The
context is ignored. Useful for long-lived API keys and unit tests.

### `WithBearerToken(src TokenSource) relay.Option`

Returns a relay option that injects `Authorization: Bearer <token>` before
every request. The token is obtained by calling `src.Token` with the request
context. If `Token` returns an error the request is aborted immediately with
that error wrapped under `"oidc: fetch bearer token"`.

### `RefreshingTokenSource(clientID, clientSecret, tokenURL string) TokenSource`

Uses the OAuth 2.0 client credentials grant (`golang.org/x/oauth2/clientcredentials`)
to fetch and automatically refresh access tokens. Tokens are cached until they
expire; refresh is transparent to callers.

### `OAuthTokenSource(ts oauth2.TokenSource) TokenSource`

Wraps any `oauth2.TokenSource` as a relay `TokenSource`. This lets you use the
full `golang.org/x/oauth2` ecosystem (OIDC discovery, JWT bearer assertion,
device flow, etc.) without coupling relay to a specific grant type.

---

## Custom `TokenSource`

Implement the interface directly for full control:

```go
type vaultTokenSource struct {
    client *vault.Client
    role   string
}

func (v *vaultTokenSource) Token(ctx context.Context) (string, error) {
    secret, err := v.client.Auth().Token().LookupSelfWithContext(ctx)
    if err != nil {
        return "", fmt.Errorf("vault token lookup: %w", err)
    }
    return secret.Auth.ClientToken, nil
}

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relayoidc.WithBearerToken(&vaultTokenSource{client: vc, role: "my-role"}),
)
```

---

## Error Handling

When `Token` returns an error, relay surfaces it directly from `Execute`:

```go
_, err := client.Execute(client.Get("/protected"))
if err != nil {
    // err wraps the original TokenSource error
    var tokenErr *oidc.TokenError
    if errors.As(err, &tokenErr) {
        log.Printf("token unavailable: %v", tokenErr)
    }
}
```

---

## See Also

- [OAuth2 Extension](oauth.md) -- full OAuth2 client credentials with custom caching
- relay core documentation -- `WithOnBeforeRequest` hooks
