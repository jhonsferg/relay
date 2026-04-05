# Rate Limiting

relay provides a client-side rate limiter that throttles outgoing requests to protect upstream services and stay within API quotas.

## Configuration

```go
import "github.com/jhonsferg/relay"

client := relay.New(relay.Config{
    RateLimiter: relay.RateLimiterConfig{
        Enabled: true,
        RPS:     100,       // requests per second
        Burst:   20,        // burst allowance
    },
})
```

The underlying implementation uses a token-bucket algorithm. `RPS` sets the refill rate and `Burst` sets the bucket capacity.

## Strategies

### Token bucket (default)

Allows short bursts then smooths out to `RPS`:

```go
client := relay.New(relay.Config{
    RateLimiter: relay.RateLimiterConfig{
        Enabled:  true,
        RPS:      50,
        Burst:    10,
        Strategy: relay.TokenBucket, // default
    },
})
```

### Fixed window

Hard limit of N requests per second window. No burst allowed:

```go
client := relay.New(relay.Config{
    RateLimiter: relay.RateLimiterConfig{
        Enabled:  true,
        RPS:      30,
        Strategy: relay.FixedWindow,
    },
})
```

### Leaky bucket

Requests are queued and emitted at a constant rate:

```go
client := relay.New(relay.Config{
    RateLimiter: relay.RateLimiterConfig{
        Enabled:   true,
        RPS:       20,
        QueueSize: 100,  // max queued requests
        Strategy:  relay.LeakyBucket,
    },
})
```

## Behaviour when limit is reached

By default, relay blocks the goroutine until a token is available. Configure a maximum wait time to return an error instead:

```go
client := relay.New(relay.Config{
    RateLimiter: relay.RateLimiterConfig{
        Enabled:    true,
        RPS:        10,
        Burst:      2,
        MaxWait:    500 * time.Millisecond, // error if no token within 500ms
    },
})
```

When `MaxWait` is exceeded `relay.ErrRateLimitExceeded` is returned:

```go
resp, err := client.R().GET(ctx, "/v1/data")
if errors.Is(err, relay.ErrRateLimitExceeded) {
    log.Println("rate limit reached, back off and retry later")
}
```

## Adaptive rate limiting

Automatically reduce the rate when upstream signals throttling (HTTP 429):

```go
client := relay.New(relay.Config{
    RateLimiter: relay.RateLimiterConfig{
        Enabled:  true,
        RPS:      100,
        Adaptive: true,     // reduce RPS on 429, recover gradually
    },
    MaxRetries:       3,
    HonourRetryAfter: true,
})
```

With `Adaptive: true`, relay:
1. Halves `RPS` on each 429 response.
2. Gradually increases RPS back to the configured maximum over 30 seconds of successful responses.

## Per-host rate limiting

Apply separate limits per upstream host:

```go
client := relay.New(relay.Config{
    RateLimiter: relay.RateLimiterConfig{
        Enabled: true,
        PerHost: true,
        HostLimits: map[string]relay.HostRateLimit{
            "api.example.com":    {RPS: 100, Burst: 20},
            "search.example.com": {RPS: 20, Burst: 5},
        },
        Default: relay.HostRateLimit{RPS: 50, Burst: 10},
    },
})
```

## Request-level bypass

Skip rate limiting for a single high-priority request:

```go
resp, err := client.R().
    BypassRateLimit().
    GET(ctx, "/v1/health")
```

!!! warning
    Use `BypassRateLimit` sparingly. It can cause 429 errors from the upstream if overused.

## Metrics and observability

Inspect current rate limiter state at runtime:

```go
stats := client.RateLimiterStats()
log.Printf("current RPS: %.1f, tokens available: %d, queued: %d",
    stats.CurrentRPS, stats.Available, stats.Queued)
```

Hook into state changes:

```go
client := relay.New(relay.Config{
    RateLimiter: relay.RateLimiterConfig{
        Enabled: true,
        RPS:     100,
        OnThrottle: func(wait time.Duration) {
            metrics.ThrottledRequests.Inc()
            log.Printf("rate limited - waiting %s", wait)
        },
    },
})
```

## Full example

```go
package main

import (
    "context"
    "errors"
    "log"
    "sync"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.Config{
        BaseURL: "https://api.example.com",
        RateLimiter: relay.RateLimiterConfig{
            Enabled:  true,
            RPS:      20,
            Burst:    5,
            MaxWait:  2 * time.Second,
            Adaptive: true,
        },
        MaxRetries:       3,
        HonourRetryAfter: true,
    })

    var wg sync.WaitGroup
    for i := range 100 {
        wg.Add(1)
        go func(n int) {
            defer wg.Done()
            var result map[string]any
            _, err := client.R().
                SetResult(&result).
                GET(context.Background(), "/v1/items")
            if errors.Is(err, relay.ErrRateLimitExceeded) {
                log.Printf("request %d: rate limit wait exceeded", n)
                return
            }
            if err != nil {
                log.Printf("request %d: %v", n, err)
                return
            }
            log.Printf("request %d: ok", n)
        }(i)
    }
    wg.Wait()
}
```

## See also

- [Retries & Backoff](retries.md)
- [Circuit Breaker](circuit-breaker.md)
- [Bulkhead Isolation](../features/bulkhead.md)
- [Request Hedging](../features/hedging.md)
