# Circuit Breaker

relay's circuit breaker prevents cascading failures by stopping requests to an unhealthy upstream automatically and allowing traffic again only after a recovery probe succeeds.

## How it works

The circuit breaker operates as a state machine with three states:

```
CLOSED ──[failure threshold]──► OPEN ──[timeout]──► HALF-OPEN
  ▲                                                      │
  └──────────[probe succeeds]───────────────────────────┘
```

| State | Behaviour |
|-------|-----------|
| **Closed** | Requests pass through normally. Failures are counted. |
| **Open** | All requests fail immediately with `ErrCircuitOpen`. No network calls. |
| **Half-Open** | One probe request is allowed. Success closes the circuit; failure keeps it open. |

## Configuration

```go
import "github.com/jhonsferg/relay"

client := relay.New(relay.Config{
    CircuitBreaker: relay.CircuitBreakerConfig{
        Enabled:          true,
        FailureThreshold: 5,             // open after 5 consecutive failures
        SuccessThreshold: 2,             // close after 2 consecutive successes in half-open
        Timeout:          30 * time.Second, // stay open for 30s before probing
    },
})
```

## Default values

| Field | Default |
|-------|---------|
| `FailureThreshold` | `5` |
| `SuccessThreshold` | `1` |
| `Timeout` | `60s` |

## Detecting circuit-open errors

```go
resp, err := client.R().GET(ctx, "/service/health")
if errors.Is(err, relay.ErrCircuitOpen) {
    log.Println("circuit is open - service unavailable, using fallback")
    return fallbackData()
}
```

## Per-host circuit breakers

relay maintains a separate circuit breaker per host, so a slow upstream does not affect requests to other hosts on the same client:

```go
client := relay.New(relay.Config{
    CircuitBreaker: relay.CircuitBreakerConfig{
        Enabled:          true,
        FailureThreshold: 3,
        Timeout:          20 * time.Second,
        PerHost:          true, // default: true
    },
})

// These two upstreams have independent circuit states
client.R().GET(ctx, "https://api-a.example.com/data")
client.R().GET(ctx, "https://api-b.example.com/data")
```

## Failure classification

By default any network error or 5xx response counts as a failure. Customise with `IsFailure`:

```go
client := relay.New(relay.Config{
    CircuitBreaker: relay.CircuitBreakerConfig{
        Enabled:          true,
        FailureThreshold: 5,
        IsFailure: func(resp *http.Response, err error) bool {
            if err != nil {
                return true
            }
            // 500 is a failure, but 503 means maintenance - don't trip the breaker
            return resp.StatusCode == 500 || resp.StatusCode == 502
        },
    },
})
```

## Observability hooks

Monitor state transitions with event hooks:

```go
client := relay.New(relay.Config{
    CircuitBreaker: relay.CircuitBreakerConfig{
        Enabled:          true,
        FailureThreshold: 5,
        Timeout:          30 * time.Second,
        OnStateChange: func(host, from, to string) {
            log.Printf("circuit breaker [%s]: %s -> %s", host, from, to)
            metrics.CircuitState.WithLabelValues(host, to).Set(1)
        },
    },
})
```

## Combining with retries

The circuit breaker sits in front of the retry engine. Once the circuit is open, retries are skipped entirely - a fast fail without exhausting retry budget:

```go
client := relay.New(relay.Config{
    MaxRetries: 3,
    CircuitBreaker: relay.CircuitBreakerConfig{
        Enabled:          true,
        FailureThreshold: 5,
        Timeout:          30 * time.Second,
    },
})
```

!!! tip "Recommended pattern"
    Use the circuit breaker together with retries and a fallback. Retries handle transient blips; the circuit breaker handles sustained outages; the fallback serves stale or degraded data.

## Manual control

Force the circuit into a specific state for testing or maintenance:

```go
cb := client.CircuitBreaker("api.example.com")

cb.ForceOpen()    // immediately reject all requests
cb.ForceClose()   // allow all requests (ignore failures)
cb.Reset()        // return to normal (closed) state
```

## Full example

```go
package main

import (
    "context"
    "errors"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func fetchWithFallback(ctx context.Context, client *relay.Client) ([]byte, error) {
    var body []byte
    _, err := client.R().
        SetResult(&body).
        GET(ctx, "/v1/products")

    if errors.Is(err, relay.ErrCircuitOpen) {
        log.Println("circuit open - returning cached data")
        return cachedProducts(), nil
    }
    return body, err
}

func main() {
    client := relay.New(relay.Config{
        BaseURL:    "https://api.example.com",
        MaxRetries: 2,
        CircuitBreaker: relay.CircuitBreakerConfig{
            Enabled:          true,
            FailureThreshold: 5,
            SuccessThreshold: 2,
            Timeout:          30 * time.Second,
            OnStateChange: func(host, from, to string) {
                log.Printf("CB %s: %s -> %s", host, from, to)
            },
        },
    })

    data, err := fetchWithFallback(context.Background(), client)
    if err != nil {
        log.Fatal(err)
    }
    _ = data
}

func cachedProducts() []byte { return nil }
```

## See also

- [Retries & Backoff](retries.md)
- [Rate Limiting](rate-limiting.md)
- [Bulkhead Isolation](../features/bulkhead.md)
