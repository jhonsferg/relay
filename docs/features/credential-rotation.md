# Credential Rotation

relay supports dynamic credential injection via the `CredentialProvider` interface. A provider is called before every request attempt (including retries), so short-lived tokens, rotating API keys, and vault-fetched secrets are always fresh without restarting the process.

---

## Usage

### Static Credentials

For a fixed token or API key that never changes:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    provider := relay.StaticCredentialProvider(relay.Credentials{
        BearerToken: "my-static-token",
    })

    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithCredentialProvider(provider),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/protected", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

### Rotating Token (OAuth / JWT)

For tokens that expire and need to be refreshed automatically:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

// fetchToken calls your OAuth token endpoint and returns the token + expiry.
func fetchToken(ctx context.Context) (string, time.Time, error) {
    // ... call your auth server ...
    return "eyJhbG...", time.Now().Add(55 * time.Minute), nil
}

func main() {
    provider := relay.NewRotatingTokenProvider(
        fetchToken,
        5*time.Minute, // refresh when within 5 minutes of expiry
    )

    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithCredentialProvider(provider),
    )
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()
    resp, err := client.Get(ctx, "/protected", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

### Custom Provider

Implement `CredentialProvider` directly for vault secrets, SPIFFE SVIDs, or any other source:

```go
type VaultProvider struct {
    client *vault.Client
}

func (v *VaultProvider) Credentials(ctx context.Context) (relay.Credentials, error) {
    secret, err := v.client.KVv2("secret").Get(ctx, "api-key")
    if err != nil {
        return relay.Credentials{}, err
    }
    return relay.Credentials{
        Headers: map[string]string{
            "X-API-Key": secret.Data["value"].(string),
        },
    }, nil
}
```

---

## API Reference

### `CredentialProvider` Interface

```go
type CredentialProvider interface {
    Credentials(ctx context.Context) (Credentials, error)
}
```

### `Credentials`

```go
type Credentials struct {
    BearerToken string            // sets Authorization: Bearer <token>
    BasicAuth   *BasicAuthCreds   // sets HTTP basic auth
    Headers     map[string]string // arbitrary headers (e.g. X-API-Key)
}
```

All three fields are optional and can be combined. `BearerToken` takes precedence over `BasicAuth` when both are set.

### `BasicAuthCreds`

```go
type BasicAuthCreds struct {
    Username string
    Password string
}
```

### `StaticCredentialProvider`

```go
func StaticCredentialProvider(creds Credentials) CredentialProvider
```

Returns a provider that always returns the same credentials.

### `NewRotatingTokenProvider`

```go
func NewRotatingTokenProvider(
    refresh func(ctx context.Context) (token string, expiry time.Time, err error),
    threshold time.Duration,
) *RotatingTokenProvider
```

Returns a provider that caches a bearer token and calls `refresh` whenever the token is within `threshold` of its expiry. The first call always triggers a refresh (the internal token starts empty). Concurrent callers block only during the actual refresh.

### `WithCredentialProvider`

```go
func WithCredentialProvider(p CredentialProvider) Option
```

Attaches a provider to the client. Called before every request attempt, including retries.

---

## Notes

- When both a `CredentialProvider` and a `RequestSigner` (`WithSigner`) are configured, the provider runs first; the signer can then sign the final headers.
- `RotatingTokenProvider` serialises concurrent refresh calls via an internal mutex so only one goroutine calls `refresh` at a time.
- If `refresh` returns an error, the request fails immediately with that error rather than using stale credentials.
