# Authentication

relay supports all common authentication schemes out of the box. Authentication options are configured at the client level and applied automatically to every request.

## Bearer Token

The most common authentication method for modern REST APIs. Sets the `Authorization: Bearer <token>` header on every request.

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/jhonsferg/relay"
)

type Profile struct {
    ID    int    `json:"id"`
    Login string `json:"login"`
    Name  string `json:"name"`
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.github.com"),
        relay.WithBearerToken("ghp_your_personal_access_token_here"),
        relay.WithTimeout(10 * time.Second),
    )

    req := client.Get("/user")
    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        panic(err)
    }

    profile, err := relay.DecodeJSON[Profile](resp)
    if err != nil {
        panic(err)
    }
    fmt.Printf("Logged in as: %s (%s)\n", profile.Login, profile.Name)
}
```

!!! tip "Token rotation"
    If your token expires, create a new client or use `WithOAuth2` for automatic token refresh. For dynamic tokens, consider a custom auth hook (see [Custom Auth via Hooks](#custom-auth-via-hooks) below).

## Basic Authentication

Sends credentials as `Authorization: Basic <base64(user:pass)>`. Suitable for APIs that support HTTP Basic Auth.

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithBasicAuth("myuser", "mysecretpassword"),
        relay.WithTimeout(10 * time.Second),
    )

    req := client.Get("/protected-resource")
    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    fmt.Println("Status:", resp.StatusCode)
}
```

!!! warning "Security"
    Basic Auth transmits credentials (base64-encoded, not encrypted) with every request. Always use HTTPS when using Basic Auth. Never use Basic Auth over plain HTTP.

## API Key Authentication

Many APIs use a custom header or query parameter for API key auth. relay supports header-based API keys via `WithAPIKey`.

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/jhonsferg/relay"
)

type WeatherResponse struct {
    Temperature float64 `json:"temperature"`
    Condition   string  `json:"condition"`
    City        string  `json:"city"`
}

func main() {
    // API key in a custom header
    client := relay.New(
        relay.WithBaseURL("https://api.weather.example.com/v1"),
        relay.WithAPIKey("X-API-Key", "wk_live_abc123xyz789"),
        relay.WithTimeout(10 * time.Second),
    )

    req := client.Get("/current").WithQueryParam("city", "London")
    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        panic(err)
    }

    weather, err := relay.DecodeJSON[WeatherResponse](resp)
    if err != nil {
        panic(err)
    }
    fmt.Printf("%s: %.1f degrees, %s\n", weather.City, weather.Temperature, weather.Condition)
}
```

For API keys passed as query parameters, use a hook:

```go
client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relay.WithBeforeRetryHook(func(req *relay.Request, attempt int, err error) {
        // This pattern shows how you can inject per-retry logic
    }),
)
```

Or simply append it in each request:

```go
req := client.Get("/data").WithQueryParam("api_key", "your_key_here")
```

## Digest Authentication

HTTP Digest Auth is a challenge-response authentication mechanism. relay handles the full challenge-response handshake automatically.

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithDigestAuth("admin", "password123"),
        relay.WithTimeout(15 * time.Second),
    )

    req := client.Get("/protected/resource")
    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    fmt.Println("Status:", resp.StatusCode)
}
```

!!! note "How Digest Auth works"
    relay sends the initial request, receives the 401 challenge with the `WWW-Authenticate: Digest` header, computes the response hash (MD5 or SHA-256 depending on server algorithm), and retransmits automatically. This is transparent to the caller.

## OAuth2 - Client Credentials Flow

For server-to-server authentication where your application is the resource owner. relay fetches and caches the access token automatically, refreshing it before expiry.

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/jhonsferg/relay"
)

type ServiceData struct {
    Records []struct {
        ID   string `json:"id"`
        Name string `json:"name"`
    } `json:"records"`
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.service.example.com"),
        relay.WithOAuth2(&relay.OAuth2Config{
            ClientID:     "my-client-id",
            ClientSecret: "my-client-secret",
            TokenURL:     "https://auth.service.example.com/oauth/token",
            Scopes:       []string{"read:data", "write:data"},
        }),
        relay.WithTimeout(15 * time.Second),
    )

    req := client.Get("/v1/records")
    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        panic(err)
    }

    data, err := relay.DecodeJSON[ServiceData](resp)
    if err != nil {
        panic(err)
    }
    fmt.Printf("Fetched %d records\n", len(data.Records))
}
```

The `OAuth2Config` struct fields:

| Field | Type | Description |
|-------|------|-------------|
| `ClientID` | `string` | OAuth2 client identifier |
| `ClientSecret` | `string` | OAuth2 client secret |
| `TokenURL` | `string` | Token endpoint URL |
| `Scopes` | `[]string` | Requested permission scopes |
| `GrantType` | `string` | Defaults to `"client_credentials"` |

!!! tip "Token caching"
    relay caches the access token in memory and automatically refreshes it when it expires (before the `expires_in` deadline). No extra code needed.

## HMAC Request Signing

HMAC signing generates a cryptographic signature of the request and attaches it as a header. This proves the request was not tampered with in transit and came from a holder of the shared secret.

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    // Signs each request with HMAC-SHA256
    // The signature is placed in the X-Signature header by default
    client := relay.New(
        relay.WithBaseURL("https://api.partner.example.com"),
        relay.WithHMACSign("my-shared-secret-key", "sha256"),
        relay.WithTimeout(10 * time.Second),
    )

    req := client.Post("/events").WithBody(map[string]string{
        "event": "order.created",
        "id":    "ord_12345",
    })

    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    fmt.Println("Event sent, status:", resp.StatusCode)
}
```

Supported algorithms:
- `"sha256"` - HMAC-SHA256 (recommended)
- `"sha512"` - HMAC-SHA512
- `"sha1"` - HMAC-SHA1 (legacy, avoid for new integrations)

!!! note "HMAC vs SigV4"
    HMAC signing is for generic partner APIs that use custom signing schemes. For AWS services, use the dedicated AWS SigV4 extension: [ext/sigv4](../extensions/sigv4.md).

## Custom Auth via Hooks

For custom authentication requirements - such as rotating JWT tokens from a secrets manager, adding multiple auth headers, or implementing proprietary schemes - use a `BeforeRetryHook` or wrap at the transport layer.

```go
package main

import (
    "context"
    "fmt"
    "sync"
    "time"

    "github.com/jhonsferg/relay"
)

// TokenManager refreshes a token when it expires.
type TokenManager struct {
    mu      sync.RWMutex
    token   string
    expires time.Time
}

func (tm *TokenManager) Token() string {
    tm.mu.RLock()
    defer tm.mu.RUnlock()
    return tm.token
}

func (tm *TokenManager) Refresh() error {
    tm.mu.Lock()
    defer tm.mu.Unlock()
    // In real code: call your auth endpoint here
    tm.token = "new-dynamic-token-" + time.Now().Format("150405")
    tm.expires = time.Now().Add(1 * time.Hour)
    return nil
}

func (tm *TokenManager) IsExpired() bool {
    tm.mu.RLock()
    defer tm.mu.RUnlock()
    return time.Now().After(tm.expires)
}

func main() {
    tm := &TokenManager{}
    _ = tm.Refresh() // initial fetch

    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTimeout(10 * time.Second),
        // Inject the current token before each retry (and first attempt)
        relay.WithBeforeRetryHook(func(req *relay.Request, attempt int, err error) {
            if tm.IsExpired() {
                if refreshErr := tm.Refresh(); refreshErr != nil {
                    fmt.Println("Token refresh failed:", refreshErr)
                    return
                }
            }
            req.WithHeader("Authorization", "Bearer "+tm.Token())
        }),
    )

    req := client.Get("/secure/data")
    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    fmt.Println("Status:", resp.StatusCode)
}
```

!!! tip "Per-request auth"
    You can override or add auth headers on individual requests using `req.WithHeader("Authorization", "Bearer "+token)`. This overrides the client-level auth for that single request.

## Combining Auth with Other Options

Auth options compose naturally with all other relay options:

```go
client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relay.WithBearerToken("your-token"),
    relay.WithRetry(&relay.RetryConfig{
        MaxAttempts:     3,
        WaitMin:         500 * time.Millisecond,
        WaitMax:         5 * time.Second,
        RetryableStatus: []int{429, 500, 502, 503, 504},
    }),
    relay.WithTimeout(30 * time.Second),
    relay.WithMaxConcurrentRequests(20),
)
```

## Auth in Extension Modules

For advanced OAuth2 flows (PKCE, authorization code), use the dedicated extension:

```go
import "github.com/jhonsferg/relay/ext/oauth"

// See /extensions/oauth for full documentation
```

For AWS services:

```go
import "github.com/jhonsferg/relay/ext/sigv4"

// See /extensions/sigv4 for full documentation
```

## Next Steps

- [Retries](retries.md) - Retry failed requests automatically
- [Hooks](hooks.md) - Full hook documentation including BeforeRetryHook
- [Extensions: OAuth2](../extensions/oauth.md) - Authorization code flow with PKCE
- [Extensions: AWS SigV4](../extensions/sigv4.md) - AWS service authentication
