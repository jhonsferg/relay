# Chaos Engineering Extension

The `ext/chaos` extension injects controlled faults into HTTP requests for **testing resilience**. Use it to verify that your application handles network errors, latency, and server failures gracefully.

!!! warning "Never use in production"
    The chaos middleware is designed exclusively for tests and staging environments.

## Installation

```bash
go get github.com/jhonsferg/relay/ext/chaos
```

## Quick Start

```go
import "github.com/jhonsferg/relay/ext/chaos"

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    chaos.Middleware(chaos.Config{
        ErrorRate:   0.10, // 10% of requests return an error
        LatencyRate: 0.20, // 20% of requests get extra latency
        Latency:     50 * time.Millisecond,
        FaultRate:   0.05, // 5% return a fault status code
        Faults:      []int{503, 500},
    }),
)
```

## Configuration

```go
type Config struct {
    // ErrorRate is the probability [0.0, 1.0] of returning a synthetic error.
    ErrorRate float64

    // LatencyRate is the probability [0.0, 1.0] of injecting artificial latency.
    LatencyRate float64

    // Latency is the duration added when LatencyRate triggers.
    Latency time.Duration

    // Faults is a list of HTTP status codes to randomly inject.
    Faults []int

    // FaultRate is the probability [0.0, 1.0] of injecting a fault status code.
    FaultRate float64
}
```

## Fault Types

### Synthetic Errors

Returns `chaos.ErrChaosInjected` without making a real HTTP call:

```go
chaos.Middleware(chaos.Config{
    ErrorRate: 1.0, // always fail
})
```

### Artificial Latency

Delays the request by a fixed duration before proceeding:

```go
chaos.Middleware(chaos.Config{
    LatencyRate: 0.50,                // 50% of requests
    Latency:     200 * time.Millisecond,
})
```

Latency respects context cancellation - if the context is cancelled during the delay, the request returns `context.Canceled`.

### Fault Status Codes

Returns a synthetic HTTP response with the given status code:

```go
chaos.Middleware(chaos.Config{
    FaultRate: 0.30,
    Faults:    []int{500, 502, 503}, // randomly selected
})
```

## Testing Example

```go
func TestServiceResilience(t *testing.T) {
    // Simulate 20% error rate
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithRetry(&relay.RetryConfig{
            MaxAttempts:     3,
            RetryableStatus: []int{503},
        }),
        chaos.Middleware(chaos.Config{
            FaultRate: 0.20,
            Faults:    []int{503},
        }),
    )

    // Your service should handle this gracefully
    resp, err := client.Execute(client.Get("/health"))
    if err != nil {
        t.Logf("expected: service handled error: %v", err)
    }
}
```

## Combining Faults

All fault types are evaluated independently per request:

```go
chaos.Middleware(chaos.Config{
    ErrorRate:   0.05, // 5% → synthetic error
    LatencyRate: 0.15, // 15% → added latency (then continues)
    FaultRate:   0.10, // 10% → fault status code
    Faults:      []int{500, 503},
})
```

Note: Latency is injected first; if an error is also triggered, the request returns the error after the latency.

## ErrChaosInjected

```go
import "errors"

resp, err := client.Execute(client.Get("/api"))
if errors.Is(err, chaos.ErrChaosInjected) {
    // A synthetic error was injected by the chaos middleware
}
```
