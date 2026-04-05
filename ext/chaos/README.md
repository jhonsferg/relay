# relay/ext/chaos

Fault-injection middleware for [relay](https://github.com/jhonsferg/relay) HTTP clients.

Use this extension in tests and staging environments to validate your application's
resilience against network failures, latency spikes, and error status codes.

> **WARNING**: Never use this in production.

## Installation

```sh
go get github.com/jhonsferg/relay/ext/chaos
```

## Usage

```go
import (
    "github.com/jhonsferg/relay"
    chaos "github.com/jhonsferg/relay/ext/chaos"
)

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    chaos.Middleware(chaos.Config{
        // 10% chance of returning a synthetic error
        ErrorRate: 0.10,
        // 20% chance of adding 200ms of latency
        LatencyRate: 0.20,
        Latency:     200 * time.Millisecond,
        // 5% chance of returning a fault status code
        FaultRate: 0.05,
        Faults:    []int{503, 502, 504},
    }),
)
```

## Config fields

| Field | Type | Description |
|-------|------|-------------|
| `ErrorRate` | `float64` | Probability [0.0, 1.0] of returning `ErrChaosInjected` |
| `LatencyRate` | `float64` | Probability [0.0, 1.0] of injecting artificial latency |
| `Latency` | `time.Duration` | Duration of injected latency when `LatencyRate` triggers |
| `FaultRate` | `float64` | Probability [0.0, 1.0] of injecting a fault status code |
| `Faults` | `[]int` | HTTP status codes to randomly inject (uniform distribution) |

Faults are applied in order: latency - error - fault status code.
