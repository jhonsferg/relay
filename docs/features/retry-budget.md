# Retry Budget

A retry budget prevents **retry storms** - the cascading failure pattern where every request starts retrying simultaneously when a downstream service degrades, multiplying load on an already struggling backend.

## The Problem

Without a retry budget, a single degraded service can trigger:

```
100 requests × 3 retries = 300 total requests hitting an already overloaded service
```

This amplifies traffic at exactly the worst moment.

## Solution

The retry budget tracks attempts and retries in a **sliding time window**. When the retry fraction exceeds the configured ratio, further retries are suppressed with `ErrRetryBudgetExhausted`.

## Configuration

```go
client := relay.New(
    relay.WithRetryBudget(&relay.RetryBudget{
        Ratio:    0.10,             // at most 10% of requests may be retried
        Window:   10 * time.Second, // sliding observation window
        MinRetry: 10,               // always allow at least 10 retries
    }),
)
```

| Field | Description | Default |
|-------|-------------|---------|
| `Ratio` | Max fraction of requests that can be retried (0.0-1.0) | required |
| `Window` | Sliding time window for observation | required |
| `MinRetry` | Minimum retries always allowed regardless of ratio | 10 |

## How It Works

The tracker maintains a sliding window of events:

```
Window: [attempt, attempt, attempt, retry, attempt, retry, ...]
                                     ^--budget--^
```

A retry is allowed when:
- `retries < MinRetry` (minimum budget always available), OR
- `(retries + 1) / (total + 1) <= Ratio`

Otherwise, `ErrRetryBudgetExhausted` is returned immediately.

## Example

```go
import "errors"

client := relay.New(
    relay.WithRetry(&relay.RetryConfig{
        MaxAttempts:     3,
        InitialInterval: 100 * time.Millisecond,
        RetryableStatus: []int{429, 500, 502, 503, 504},
    }),
    relay.WithRetryBudget(&relay.RetryBudget{
        Ratio:    0.10,
        Window:   10 * time.Second,
        MinRetry: 10,
    }),
)

resp, err := client.Execute(client.Get("/api/data"))
if errors.Is(err, relay.ErrRetryBudgetExhausted) {
    // Budget exhausted - fail fast, don't retry
    log.Warn("retry budget exhausted, serving degraded response")
}
```

## Combining with Circuit Breaker

For maximum resilience, combine retry budget with the circuit breaker:

```go
client := relay.New(
    relay.WithRetryBudget(&relay.RetryBudget{
        Ratio:    0.10,
        Window:   10 * time.Second,
        MinRetry: 5,
    }),
    relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
        MaxFailures:  10,
        ResetTimeout: 30 * time.Second,
    }),
)
```

- **Retry budget**: prevents amplification during degradation
- **Circuit breaker**: opens the circuit after sustained failures

## Thread Safety

The `retryBudgetTracker` is shared across all concurrent requests on a client and is fully goroutine-safe using a mutex-protected sliding window.
