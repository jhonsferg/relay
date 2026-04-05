# Load Balancer

relay's built-in client-side load balancer distributes requests across multiple backend URLs. When configured, each request selects a backend from the pool instead of using a single `BaseURL`.

## Strategies

| Strategy | Description |
|----------|-------------|
| `RoundRobin` (default) | Cycles through backends sequentially using an atomic counter |
| `Random` | Selects a backend uniformly at random |

## Quick Start

```go
client := relay.New(
    relay.WithLoadBalancer(relay.LoadBalancerConfig{
        Backends: []string{
            "https://api1.example.com",
            "https://api2.example.com",
            "https://api3.example.com",
        },
        Strategy: relay.RoundRobin,
    }),
)

// Requests are distributed: api1, api2, api3, api1, api2, ...
resp, err := client.Execute(client.Get("/users"))
```

## Round Robin

The default strategy uses an atomic counter with zero lock contention:

```go
client := relay.New(
    relay.WithLoadBalancer(relay.LoadBalancerConfig{
        Backends: []string{
            "https://us-east.api.example.com",
            "https://us-west.api.example.com",
            "https://eu.api.example.com",
        },
        // Strategy defaults to RoundRobin
    }),
)
```

Request distribution:
```
Request 1 → us-east
Request 2 → us-west
Request 3 → eu
Request 4 → us-east  (cycle restarts)
```

## Random

For workloads where sequential distribution is undesirable:

```go
client := relay.New(
    relay.WithLoadBalancer(relay.LoadBalancerConfig{
        Backends: []string{
            "https://node1.example.com",
            "https://node2.example.com",
        },
        Strategy: relay.Random,
    }),
)
```

## Configuration

```go
type LoadBalancerConfig struct {
    // Backends is the list of base URLs to balance across.
    Backends []string
    // Strategy selects the balancing algorithm.
    // Defaults to RoundRobin if not set.
    Strategy LoadBalancerStrategy
}
```

## Path Handling

Request paths are appended to the selected backend URL:

```go
client := relay.New(
    relay.WithLoadBalancer(relay.LoadBalancerConfig{
        Backends: []string{"https://api1.example.com", "https://api2.example.com"},
    }),
)

// GET /users/42 will be sent to either:
// https://api1.example.com/users/42
// https://api2.example.com/users/42
resp, err := client.Execute(client.Get("/users/42"))
```

## Combining with Other Features

The load balancer composes with all other relay features:

```go
client := relay.New(
    relay.WithLoadBalancer(relay.LoadBalancerConfig{
        Backends: []string{
            "https://primary.api.example.com",
            "https://secondary.api.example.com",
        },
    }),
    relay.WithRetry(&relay.RetryConfig{
        MaxAttempts:     3,
        RetryableStatus: []int{502, 503, 504},
    }),
    relay.WithRetryBudget(&relay.RetryBudget{
        Ratio:  0.10,
        Window: 10 * time.Second,
    }),
    relay.WithCircuitBreaker(nil), // default circuit breaker
)
```

## Thread Safety

The load balancer is fully goroutine-safe. Round-robin uses `sync/atomic` for the counter - no lock contention even under high concurrency.

## Limitations

- No health checking: unhealthy backends are not automatically removed (combine with circuit breaker for failure isolation)
- No weighted distribution: all backends receive equal traffic
- No sticky sessions: requests from the same client may go to different backends
