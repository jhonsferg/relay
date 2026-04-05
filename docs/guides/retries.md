# Retries & Backoff

relay includes a built-in retry engine with configurable backoff strategies, jitter, and per-status-code policies. No third-party packages required.

## Basic retry configuration

```go
import "github.com/jhonsferg/relay"

client := relay.New(relay.Config{
    MaxRetries:      3,
    RetryWaitMin:    500 * time.Millisecond,
    RetryWaitMax:    5 * time.Second,
    RetryableStatus: []int{429, 500, 502, 503, 504},
})
```

## Backoff strategies

### Exponential backoff (default)

Wait time doubles on each attempt: `500ms → 1s → 2s → 4s`.

```go
client := relay.New(relay.Config{
    MaxRetries:   4,
    RetryWaitMin: 200 * time.Millisecond,
    RetryWaitMax: 10 * time.Second,
    // ExponentialBackoff is the default - no extra field needed
})
```

### Constant backoff

Every retry waits the same amount:

```go
client := relay.New(relay.Config{
    MaxRetries:      3,
    RetryWaitMin:    1 * time.Second,
    RetryWaitMax:    1 * time.Second, // min == max => constant
    RetryableStatus: []int{503},
})
```

### Jitter

Add randomness to prevent thundering herd problems:

```go
client := relay.New(relay.Config{
    MaxRetries:   3,
    RetryWaitMin: 500 * time.Millisecond,
    RetryWaitMax: 5 * time.Second,
    RetryJitter:  true,
})
```

With jitter enabled, each wait is `base + rand(0, base*0.5)`.

## Retryable status codes

By default relay retries `429`, `500`, `502`, `503`, `504`. Customize per client:

```go
client := relay.New(relay.Config{
    RetryableStatus: []int{429, 503}, // only these two
})
```

## Custom retry condition

For fine-grained control implement `RetryConditionFunc`:

```go
client := relay.New(relay.Config{
    MaxRetries: 5,
    RetryCondition: func(resp *http.Response, err error) bool {
        if err != nil {
            return true // network errors are always retried
        }
        if resp.StatusCode == 429 {
            return true
        }
        // Retry on 500 only when the service signals it is transient
        if resp.StatusCode == 500 {
            return resp.Header.Get("X-Retry-Hint") == "transient"
        }
        return false
    },
})
```

## Retry hooks

Execute code before each retry attempt using hooks:

```go
client := relay.New(relay.Config{
    MaxRetries: 3,
    OnRetry: func(attempt int, req *http.Request, resp *http.Response, err error) {
        log.Printf("retry %d for %s %s", attempt, req.Method, req.URL)
    },
})
```

## Respecting Retry-After headers

When a server returns a `Retry-After` header (common with 429 responses), relay automatically honours it:

```go
client := relay.New(relay.Config{
    MaxRetries:       3,
    HonourRetryAfter: true, // enabled by default
})
```

The delay is capped at `RetryWaitMax` to avoid waiting indefinitely.

## Per-request retry override

Override retry settings for a single request without modifying the client:

```go
resp, err := client.R().
    SetRetries(5, 200*time.Millisecond, 8*time.Second).
    GET(ctx, "/unstable-endpoint")
```

## Disabling retries

```go
// Client-level
client := relay.New(relay.Config{MaxRetries: 0})

// Request-level
resp, err := client.R().NoRetry().GET(ctx, "/idempotent-check")
```

## Idempotency and safe retries

By default relay only retries idempotent HTTP methods (`GET`, `HEAD`, `OPTIONS`, `PUT`, `DELETE`). `POST` and `PATCH` are not retried unless you explicitly opt in:

```go
client := relay.New(relay.Config{
    MaxRetries:       3,
    RetryNonSafeHTTP: true, // also retry POST/PATCH
})
```

!!! warning "Data duplication risk"
    Retrying non-idempotent methods can cause duplicate writes if the server processed the original request before failing. Always use the [Idempotency Key](../features/idempotency.md) feature when enabling this option.

## Example: full retry pipeline

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.Config{
        BaseURL:      "https://api.example.com",
        MaxRetries:   4,
        RetryWaitMin: 250 * time.Millisecond,
        RetryWaitMax: 8 * time.Second,
        RetryJitter:  true,
        RetryableStatus: []int{429, 500, 502, 503, 504},
        OnRetry: func(n int, req *http.Request, _ *http.Response, err error) {
            log.Printf("[retry %d] %s %s - %v", n, req.Method, req.URL.Path, err)
        },
    })

    var result map[string]any
    _, err := client.R().
        SetResult(&result).
        GET(context.Background(), "/v1/data")
    if err != nil {
        log.Fatal(err)
    }

    log.Println("success:", result)
}
```

## See also

- [Circuit Breaker](circuit-breaker.md) - stop retrying after repeated failures
- [Rate Limiting](rate-limiting.md) - client-side request throttling
- [Idempotency](../features/idempotency.md) - safe retry keys for non-idempotent requests
