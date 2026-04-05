# OAuth2 Extension

The OAuth2 extension adds automatic token acquisition and renewal to relay clients. Instead of managing access tokens yourself, you configure the extension once and it handles every token lifecycle step: initial fetch, caching in memory, proactive refresh before expiry, and transparent retry on 401 responses.

**Import path:** `github.com/jhonsferg/relay/ext/oauth`

---

## Overview

OAuth 2.0 defines several grant types, each suited to a different authorization scenario:

- **Client Credentials** - service-to-service calls where no user is involved
- **Authorization Code + PKCE** - user-facing applications where a browser redirect is part of the flow
- **Refresh Token** - renewing access when a prior token is about to expire

The extension supports all three flows through a single `relayoauth.Config` struct. Once attached to a relay client, the extension injects a valid `Authorization: Bearer <token>` header into every outgoing request with no additional code in your request handlers.

---

## Installation

```bash
go get github.com/jhonsferg/relay/ext/oauth@latest
```

---

## Configuration

### `relayoauth.Config`

```go
type Config struct {
    // ClientID is the OAuth 2.0 client identifier issued by the authorization server.
    ClientID string

    // ClientSecret is the confidential client secret.
    // Leave empty for public clients using PKCE.
    ClientSecret string

    // TokenURL is the full URL of the token endpoint.
    // Example: "https://accounts.example.com/oauth2/token"
    TokenURL string

    // Scopes is the list of OAuth 2.0 scope strings to request.
    Scopes []string

    // GrantType selects the OAuth 2.0 flow.
    // Accepted values: "client_credentials", "authorization_code", "refresh_token".
    GrantType string

    // AuthorizationURL is required only for "authorization_code" grant type.
    // This is the URL the user's browser is redirected to.
    AuthorizationURL string

    // RedirectURL is the callback URL registered with the authorization server.
    // Required for "authorization_code" grant type.
    RedirectURL string

    // PKCE enables Proof Key for Code Exchange (RFC 7636).
    // Recommended for all authorization_code flows; mandatory for public clients.
    PKCE bool

    // ExtraParams holds additional form parameters sent with the token request.
    // Use this for authorization servers that require custom fields like "audience".
    ExtraParams map[string]string

    // RefreshBuffer is how long before token expiry the extension proactively
    // refreshes the token. Defaults to 30 seconds.
    RefreshBuffer time.Duration

    // TokenCache allows you to provide a custom token store.
    // The default in-memory cache is suitable for single-process deployments.
    // Provide a Redis-backed cache for multi-instance deployments.
    TokenCache TokenCache
}
```

### `relayoauth.WithOAuth2`

```go
relayoauth.WithOAuth2(config *relayoauth.Config) relay.Option
```

Attaches the OAuth2 extension to a relay client. The extension is lazily initialized: the first outgoing request triggers token acquisition.

---

## Client Credentials Flow

Use this flow for machine-to-machine calls where the relay client itself is the resource owner.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    relay "github.com/jhonsferg/relay"
    relayoauth "github.com/jhonsferg/relay/ext/oauth"
)

type APIResponse struct {
    Items []struct {
        ID   string `json:"id"`
        Name string `json:"name"`
    } `json:"items"`
}

func main() {
    client, err := relay.NewClient(
        relay.WithBaseURL("https://api.example.com"),
        relayoauth.WithOAuth2(&relayoauth.Config{
            ClientID:     "my-service-client-id",
            ClientSecret: "my-service-client-secret",
            TokenURL:     "https://auth.example.com/oauth2/token",
            Scopes:       []string{"api.read", "api.write"},
            GrantType:    "client_credentials",
            // Request a new token 60 seconds before the current one expires.
            RefreshBuffer: 60 * time.Second,
            // "audience" is required by some providers (e.g., Auth0, Okta).
            ExtraParams: map[string]string{
                "audience": "https://api.example.com",
            },
        }),
        relay.WithTimeout(10*time.Second),
        relay.WithRetry(relay.RetryConfig{
            MaxAttempts:     3,
            WaitBase:        500 * time.Millisecond,
            RetryableStatus: []int{429, 500, 502, 503, 504},
        }),
    )
    if err != nil {
        log.Fatalf("create client: %v", err)
    }

    var resp APIResponse
    if err := client.Get(context.Background(), "/v1/items", &resp); err != nil {
        log.Fatalf("GET /v1/items: %v", err)
    }

    for _, item := range resp.Items {
        fmt.Printf("%s: %s\n", item.ID, item.Name)
    }
}
```

> **Note:** The client credentials flow never involves a user browser redirect. The `Authorization` and `RedirectURL` fields in `Config` are not used for this grant type.

---

## Authorization Code Flow with PKCE

Use this flow when a human user must authorize access. The extension coordinates the code exchange; your application is responsible for redirecting the user to the authorization URL and capturing the callback code.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "os"
    "time"

    relay "github.com/jhonsferg/relay"
    relayoauth "github.com/jhonsferg/relay/ext/oauth"
)

func main() {
    cfg := &relayoauth.Config{
        ClientID:         os.Getenv("OAUTH_CLIENT_ID"),
        TokenURL:         "https://github.com/login/oauth/access_token",
        AuthorizationURL: "https://github.com/login/oauth/authorize",
        RedirectURL:      "http://localhost:8080/callback",
        Scopes:           []string{"repo", "read:user"},
        GrantType:        "authorization_code",
        PKCE:             true,
    }

    // Step 1: generate an authorization URL and redirect the user.
    authURL, state, err := relayoauth.BuildAuthorizationURL(cfg)
    if err != nil {
        log.Fatalf("build auth URL: %v", err)
    }
    fmt.Printf("Visit this URL to authorize:\n%s\n\n", authURL)

    // Step 2: start a local HTTP server to receive the callback.
    codeCh := make(chan string, 1)
    srv := &http.Server{Addr: ":8080"}
    http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Query().Get("state") != state {
            http.Error(w, "invalid state", http.StatusBadRequest)
            return
        }
        code := r.URL.Query().Get("code")
        codeCh <- code
        fmt.Fprintln(w, "Authorization successful! You can close this tab.")
    })
    go func() { _ = srv.ListenAndServe() }()

    // Step 3: wait for the authorization code.
    code := <-codeCh
    _ = srv.Shutdown(context.Background())

    // Step 4: exchange the code for a token and create a relay client.
    // The extension completes the PKCE verification internally.
    client, err := relay.NewClient(
        relay.WithBaseURL("https://api.github.com"),
        relayoauth.WithOAuth2(cfg),
        relayoauth.WithAuthorizationCode(code),
        relay.WithHeader("Accept", "application/vnd.github+json"),
        relay.WithTimeout(15*time.Second),
    )
    if err != nil {
        log.Fatalf("create client: %v", err)
    }

    // All subsequent requests use the token automatically.
    var user map[string]any
    if err := client.Get(context.Background(), "/user", &user); err != nil {
        log.Fatalf("GET /user: %v", err)
    }
    fmt.Printf("Logged in as: %s\n", user["login"])
}
```

> **Warning:** Never log or persist the authorization code. It is single-use and expires within minutes. After exchange, the resulting access token is stored in the extension's token cache.

---

## Automatic Token Refresh

The extension tracks the `expires_in` value from the token response and schedules a background refresh `RefreshBuffer` seconds before expiry. This happens transparently without interrupting in-flight requests.

If the refresh fails (for example due to a network partition), the extension falls back to using the existing token until it actually expires, then returns an error on the next request.

You can also trigger a manual refresh:

```go
// Force immediate token refresh regardless of expiry.
if err := relayoauth.ForceRefresh(ctx, client); err != nil {
    log.Printf("force refresh failed: %v", err)
}
```

To observe token lifecycle events, attach a token observer:

```go
client, _ := relay.NewClient(
    relay.WithBaseURL("https://api.example.com"),
    relayoauth.WithOAuth2(&relayoauth.Config{
        ClientID:  "...",
        TokenURL:  "...",
        GrantType: "client_credentials",
    }),
    relayoauth.WithTokenObserver(func(event relayoauth.TokenEvent) {
        switch event.Type {
        case relayoauth.TokenAcquired:
            log.Printf("token acquired, expires in %s", event.ExpiresIn)
        case relayoauth.TokenRefreshed:
            log.Printf("token refreshed")
        case relayoauth.TokenExpired:
            log.Printf("token expired, will re-acquire on next request")
        case relayoauth.TokenRefreshFailed:
            log.Printf("refresh failed: %v", event.Error)
        }
    }),
)
```

---

## Complete Example: GitHub API with OAuth2

This example demonstrates a complete service that lists repositories for an authenticated GitHub user using the client credentials flow equivalent for GitHub Apps (JWT + installation token exchange):

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    relay "github.com/jhonsferg/relay"
    relayoauth "github.com/jhonsferg/relay/ext/oauth"
)

type GHRepo struct {
    ID          int    `json:"id"`
    FullName    string `json:"full_name"`
    Description string `json:"description"`
    Private     bool   `json:"private"`
    Language    string `json:"language"`
    Stars       int    `json:"stargazers_count"`
    UpdatedAt   string `json:"updated_at"`
}

func main() {
    // Using GitHub's device flow for CLI tools (a variant of authorization code).
    client, err := relay.NewClient(
        relay.WithBaseURL("https://api.github.com"),
        relayoauth.WithOAuth2(&relayoauth.Config{
            ClientID:     os.Getenv("GITHUB_CLIENT_ID"),
            ClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
            TokenURL:     "https://github.com/login/oauth/access_token",
            Scopes:       []string{"repo", "read:org"},
            GrantType:    "client_credentials",
            ExtraParams: map[string]string{
                "accept": "application/json",
            },
            RefreshBuffer: 5 * time.Minute,
        }),
        relay.WithHeader("Accept", "application/vnd.github+json"),
        relay.WithHeader("X-GitHub-Api-Version", "2022-11-28"),
        relay.WithTimeout(30*time.Second),
        relay.WithRetry(relay.RetryConfig{
            MaxAttempts:     3,
            WaitBase:        1 * time.Second,
            RetryableStatus: []int{429, 500, 502, 503},
        }),
    )
    if err != nil {
        log.Fatalf("create client: %v", err)
    }

    // List user repos.
    var repos []GHRepo
    if err := client.Get(context.Background(),
        "/user/repos?sort=updated&per_page=10", &repos); err != nil {
        log.Fatalf("list repos: %v", err)
    }

    fmt.Printf("%-40s %-15s %6s  %s\n", "REPO", "LANGUAGE", "STARS", "UPDATED")
    fmt.Printf("%-40s %-15s %6s  %s\n",
        "----------------------------------------",
        "---------------", "------", "-------------------")
    for _, r := range repos {
        lang := r.Language
        if lang == "" {
            lang = "(none)"
        }
        fmt.Printf("%-40s %-15s %6d  %s\n",
            r.FullName, lang, r.Stars, r.UpdatedAt[:10])
    }
}
```

---

## Custom Token Cache

The default token cache is in-memory and scoped to a single process. For deployments with multiple replicas, implement the `TokenCache` interface to share tokens across instances:

```go
type TokenCache interface {
    // Get retrieves a stored token. Returns nil if no token exists.
    Get(ctx context.Context, key string) (*Token, error)
    // Set stores a token with the given expiry time.
    Set(ctx context.Context, key string, token *Token, expiry time.Time) error
    // Delete removes a cached token (called on force refresh).
    Delete(ctx context.Context, key string) error
}
```

Example Redis-backed cache:

```go
package main

import (
    "context"
    "encoding/json"
    "time"

    relayoauth "github.com/jhonsferg/relay/ext/oauth"
    "github.com/redis/go-redis/v9"
)

type RedisTokenCache struct {
    rdb redis.UniversalClient
}

func (c *RedisTokenCache) Get(ctx context.Context, key string) (*relayoauth.Token, error) {
    data, err := c.rdb.Get(ctx, "relay:token:"+key).Bytes()
    if err == redis.Nil {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }
    var tok relayoauth.Token
    if err := json.Unmarshal(data, &tok); err != nil {
        return nil, err
    }
    return &tok, nil
}

func (c *RedisTokenCache) Set(ctx context.Context, key string, token *relayoauth.Token, expiry time.Time) error {
    data, err := json.Marshal(token)
    if err != nil {
        return err
    }
    ttl := time.Until(expiry)
    if ttl <= 0 {
        ttl = time.Minute
    }
    return c.rdb.Set(ctx, "relay:token:"+key, data, ttl).Err()
}

func (c *RedisTokenCache) Delete(ctx context.Context, key string) error {
    return c.rdb.Del(ctx, "relay:token:"+key).Err()
}

// Usage:
//
//   rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
//   cache := &RedisTokenCache{rdb: rdb}
//   cfg := &relayoauth.Config{
//       ...,
//       TokenCache: cache,
//   }
```

> **Tip:** When deploying behind a load balancer with multiple instances, use a shared Redis token cache. This prevents every instance from independently acquiring tokens, which wastes quota and can trigger rate limits on the authorization server.

---

## Security Considerations

- Never commit `ClientSecret` or tokens to source control. Load them from environment variables or a secrets manager.
- Use PKCE for all authorization code flows, even for confidential clients. PKCE prevents authorization code interception attacks.
- Set `RefreshBuffer` to at least 30 seconds to avoid serving requests with an already-expired token.
- Validate `state` parameters in callback handlers to prevent CSRF attacks in the authorization code flow.

---

## See Also

- [gRPC Bridge Extension](grpc.md) - OAuth2 works transparently with gRPC bridge calls
- [Redis Cache Extension](cache.md) - caching API responses from OAuth-protected endpoints
- relay core documentation - adding custom middleware around token refresh events
