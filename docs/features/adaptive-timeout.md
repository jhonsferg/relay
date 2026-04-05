# Adaptive Timeout

The adaptive timeout feature automatically adjusts request timeouts based on observed response latencies, helping you avoid both wasted waits on slow endpoints and premature cancellations on variable ones.

## Overview

Static timeouts require guesswork: too short and you get false failures; too long and failures take forever to surface. Adaptive timeout measures real response times and sets the timeout dynamically, typically at a chosen percentile multiplied by a safety factor.

```
timeout = percentile(recent_latencies, p95) * multiplier
```

The result is clamped between a minimum and maximum bound.

## Configuration

```go
import "github.com/jhonsferg/relay"

client := relay.NewClient(
    relay.WithAdaptiveTimeout(relay.AdaptiveTimeoutConfig{
        Percentile:     0.95,         // use p95 latency (default)
        Multiplier:     2.0,          // 2x safety factor (default)
        WindowSize:     100,          // rolling window of 100 observations (default)
        InitialTimeout: 30 * time.Second, // timeout until enough data is collected
        MinTimeout:     1 * time.Second,
        MaxTimeout:     120 * time.Second,
    }),
)
```

All fields have sensible defaults, so a minimal config works too:

```go
client := relay.NewClient(
    relay.WithAdaptiveTimeout(relay.AdaptiveTimeoutConfig{
        InitialTimeout: 10 * time.Second,
    }),
)
```

## How It Works

1. **Warm-up phase** - until at least 5 observations are recorded, the `InitialTimeout` is used.
2. **Steady state** - each request's latency is added to a circular buffer of `WindowSize` entries. Before each request, the timeout is computed from the current buffer contents.
3. **Clamping** - the computed value is always kept within `[MinTimeout, MaxTimeout]`.
4. **Per-request override takes precedence** - if you set a timeout on the individual `Request`, adaptive timeout does not override it.

## AdaptiveTimeoutConfig Reference

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `Percentile` | `float64` | `0.95` | Percentile of the latency distribution to use (0.0-1.0) |
| `Multiplier` | `float64` | `2.0` | Safety factor applied to the percentile value |
| `WindowSize` | `int` | `100` | Number of recent observations to keep |
| `InitialTimeout` | `time.Duration` | `30s` | Timeout used during warm-up (< 5 observations) |
| `MinTimeout` | `time.Duration` | `1s` | Lower bound on computed timeout |
| `MaxTimeout` | `time.Duration` | `120s` | Upper bound on computed timeout |

## Combining with Other Timeouts

Adaptive timeout works alongside relay's global timeout. The most specific timeout wins:

```go
client := relay.NewClient(
    relay.WithTimeout(60*time.Second),           // global fallback
    relay.WithAdaptiveTimeout(relay.AdaptiveTimeoutConfig{
        InitialTimeout: 60 * time.Second,
        MinTimeout:     2 * time.Second,
        MaxTimeout:     30 * time.Second,
    }),
)

// Per-request override (adaptive ignored for this request)
resp, err := client.Execute(ctx,
    relay.NewRequest().GET("https://api.example.com/slow").
        WithTimeout(5*time.Second),
)
```

## Practical Example: Endpoint with Variable Latency

```go
client := relay.NewClient(
    relay.WithAdaptiveTimeout(relay.AdaptiveTimeoutConfig{
        Percentile:     0.99,          // be generous - use p99
        Multiplier:     1.5,
        WindowSize:     50,
        InitialTimeout: 15 * time.Second,
        MinTimeout:     500 * time.Millisecond,
        MaxTimeout:     60 * time.Second,
    }),
    relay.WithRetry(relay.RetryConfig{MaxAttempts: 3}),
)

for i := 0; i < 1000; i++ {
    resp, err := client.Execute(ctx, relay.NewRequest().GET("https://api.example.com/data"))
    // Timeout automatically tightens as p99 latency stabilises
}
```

## Thread Safety

The latency buffer is updated concurrently using atomic operations. Multiple goroutines sharing the same client record observations independently without lock contention.

## When to Use Adaptive Timeout

| Scenario | Recommendation |
|----------|----------------|
| Stable internal APIs | Static timeout sufficient |
| External APIs with variable latency | **Use adaptive timeout** |
| APIs with diurnal traffic patterns | **Use adaptive timeout** (adjusts through the day) |
| Batch processing with mixed payloads | **Use adaptive timeout** |
| Strict SLA requirements | Combine adaptive + `MaxTimeout` |
