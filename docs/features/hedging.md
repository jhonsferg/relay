# Request Hedging

Request hedging is a latency-reduction technique where a client sends a duplicate request to the same service after a short delay, and uses whichever response arrives first. The slower duplicate is cancelled as soon as the first response is received. This is sometimes called a "speculative retry" or "tail-cut" strategy.

Hedging is especially effective against tail latency - the high-percentile slow requests caused by GC pauses, kernel scheduling jitter, hot spots in downstream caches, or transient network congestion. Rather than waiting the full p99 time on every request, hedging lets you pay the cost of occasional extra network traffic to keep your observed latency close to the p50.

---

## WithHedging

```go
func WithHedging(after time.Duration) Option
```

`WithHedging` enables request hedging. After `after` duration has elapsed and the first attempt has not yet returned a response, a duplicate request is issued in parallel. The first response to arrive (from either the original or the hedge) is returned to the caller. The other in-flight request is cancelled immediately.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    // Issue a hedge if the original request has not returned within 50ms.
    // At p50 = 20ms, most requests finish before the hedge is needed.
    // At p99 = 200ms, the hedge fires and the slow outlier is replaced.
    client, err := relay.New(
        relay.WithBaseURL("https://search.internal"),
        relay.WithHedging(50*time.Millisecond),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/search?q=relay", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

> **note**
> The `after` duration should be set close to your p95 latency - not your p50 or p99. Setting it too low wastes bandwidth by hedging almost every request. Setting it too high means it only fires for extreme outliers and the latency improvement is minimal.

---

## HedgeMaxAttempts

```go
// HedgeMaxAttempts controls the maximum number of parallel attempts,
// including the original. Default is 2 (one original + one hedge).
type HedgeMaxAttempts int
```

By default, hedging issues at most two attempts: the original and one hedge. You can raise this limit to allow more parallel speculative requests, which reduces tail latency further at the cost of increased load on the downstream service.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    // Allow up to 3 parallel attempts: original + 2 hedges.
    // Second hedge fires at 2*after if neither original nor first hedge has returned.
    client, err := relay.New(
        relay.WithBaseURL("https://catalog.internal"),
        relay.WithHedging(30*time.Millisecond),
        relay.WithHedgeMaxAttempts(3),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/items/42", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

> **warning**
> Raising `HedgeMaxAttempts` above 2 multiplies the load you place on the downstream service in the tail. At `HedgeMaxAttempts(4)` you may be sending 4x the traffic in worst-case scenarios. Always confirm the downstream service has capacity before increasing this value.

---

## When to Use Hedging: Idempotent Reads Only

Hedging is safe **only** for idempotent requests where sending the same request twice produces no side effects. The canonical safe HTTP methods are:

| Method | Safe for hedging | Reason |
|---|---|---|
| GET | Yes | Reads only, no side effects |
| HEAD | Yes | Reads only, no side effects |
| OPTIONS | Yes | Metadata only, no side effects |
| PUT | Conditional | Safe only if the payload is identical and the operation is truly idempotent |
| POST | No | Creates resources, charges payments, triggers emails - sends twice = two actions |
| DELETE | No | Deleting twice is not always idempotent (e.g., second returns 404) |
| PATCH | No | Partial updates are rarely idempotent |

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

// safeSearchClient is configured for hedging - reads only.
func newSearchClient() (*relay.Client, error) {
    return relay.New(
        relay.WithBaseURL("https://search.internal"),
        relay.WithHedging(40*time.Millisecond),
        relay.WithHedgeMaxAttempts(2),
    )
}

// writesClient has no hedging - mutations only.
func newOrdersClient() (*relay.Client, error) {
    return relay.New(
        relay.WithBaseURL("https://orders.internal"),
        // No WithHedging here - POST /orders would create duplicate orders
        relay.WithTimeout(5*time.Second),
    )
}

func main() {
    search, err := newSearchClient()
    if err != nil {
        log.Fatal(err)
    }

    orders, err := newOrdersClient()
    if err != nil {
        log.Fatal(err)
    }

    // Safe: GET with hedging
    resp, err := search.Get(context.Background(), "/search?q=shoes", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    var results map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&results)
    fmt.Println("search results:", results)

    // Unsafe for hedging: POST creates an order - do NOT hedge this
    _, err = orders.Post(context.Background(), "/orders", map[string]interface{}{
        "product_id": "prod-99",
        "quantity":   1,
    })
    if err != nil {
        log.Fatal(err)
    }
}
```

---

## Why NOT to Use Hedging on Writes

Suppose you hedge a `POST /orders` request. The sequence of events could be:

1. Original request is sent at T=0ms.
2. Hedge fires at T=50ms (no response yet).
3. Original request succeeds at T=55ms, order is created in the database.
4. The hedge arrives at T=60ms, a second order is created.
5. Caller receives the first response (T=55ms) - they believe one order was placed.
6. Customer is now charged twice and two orders are shipped.

Even if the server has its own idempotency layer, relying on server-side idempotency for hedging requires explicit coordination (idempotency keys). Without that, hedging writes is dangerous.

```go
package main

import (
    "context"
    "log"

    "github.com/jhonsferg/relay"
)

// badExample demonstrates what NOT to do.
// nolint:unused
func badExample() {
    // DO NOT DO THIS - hedging POST requests is dangerous
    client, err := relay.New(
        relay.WithBaseURL("https://payments.internal"),
        relay.WithHedging(50e6), // 50ms in nanoseconds
        // ^ This would send duplicate payment requests!
    )
    if err != nil {
        log.Fatal(err)
    }

    // This POST could be executed twice, charging the customer twice.
    _, err = client.Post(context.Background(), "/charge", map[string]interface{}{
        "amount":    9999,
        "currency":  "USD",
        "card":      "tok_visa",
    })
    if err != nil {
        log.Println(err)
    }
}

func main() {
    log.Println("see badExample() for what NOT to do with hedging")
}
```

---

## Latency Percentile Improvement

Hedging's effect on latency depends on the variance of your downstream service. The more variable the latency (high p99/p50 ratio), the more hedging helps.

Consider a service with the following latency distribution:
- p50: 15ms
- p95: 40ms
- p99: 180ms
- p999: 900ms

With hedging at `after=40ms`:
- 95% of requests return within 40ms (no hedge fires)
- 5% of requests trigger a hedge
- The hedge effectively replaces the 5% slow tail with a second "fresh" attempt
- New observed p99 drops from 180ms to approximately 80ms (the p99 of the hedged window)

```go
package main

import (
    "context"
    "fmt"
    "log"
    "sort"
    "sync"
    "time"

    "github.com/jhonsferg/relay"
)

// measureLatency runs n parallel requests and reports percentiles.
func measureLatency(client *relay.Client, path string, n int) {
    latencies := make([]time.Duration, 0, n)
    var mu sync.Mutex
    var wg sync.WaitGroup

    for i := 0; i < n; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            start := time.Now()
            resp, err := client.Get(context.Background(), path, nil)
            elapsed := time.Since(start)
            if err == nil {
                resp.Body.Close()
            }
            mu.Lock()
            latencies = append(latencies, elapsed)
            mu.Unlock()
        }()
    }
    wg.Wait()

    sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
    p := func(pct float64) time.Duration {
        idx := int(float64(len(latencies)) * pct)
        if idx >= len(latencies) {
            idx = len(latencies) - 1
        }
        return latencies[idx]
    }

    fmt.Printf("p50=%-8s p95=%-8s p99=%-8s p999=%s\n",
        p(0.50), p(0.95), p(0.99), p(0.999))
}

func main() {
    // Without hedging
    plain, err := relay.New(relay.WithBaseURL("https://search.internal"))
    if err != nil {
        log.Fatal(err)
    }
    fmt.Print("without hedging: ")
    measureLatency(plain, "/search?q=test", 1000)

    // With hedging at p95 threshold
    hedged, err := relay.New(
        relay.WithBaseURL("https://search.internal"),
        relay.WithHedging(40*time.Millisecond),
    )
    if err != nil {
        log.Fatal(err)
    }
    fmt.Print("with hedging:    ")
    measureLatency(hedged, "/search?q=test", 1000)
}
```

---

## Cancellation of Losing Requests

When the first response arrives, `relay` immediately cancels the context of all other in-flight hedge requests. This is important for resource cleanup - cancelled requests do not continue consuming connections in the connection pool.

The cancellation happens automatically. The downstream service will receive a TCP RST or the connection will be closed without reading the response body. Well-behaved servers detect context cancellation and abort processing.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://api.internal"),
        relay.WithHedging(25*time.Millisecond),
        relay.WithHedgeMaxAttempts(2),
        relay.OnResponse(func(ctx context.Context, resp *relay.Response) {
            // The hedge attempt index is available in the context
            fmt.Printf("response from attempt, status=%d\n", resp.StatusCode)
        }),
        relay.OnError(func(ctx context.Context, err error) {
            if err == context.Canceled {
                // This fires for the losing hedge when it is cancelled.
                // This is expected - do not treat it as an application error.
                fmt.Println("hedge attempt cancelled (expected)")
                return
            }
            log.Println("unexpected error:", err)
        }),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/data/report", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("final status:", resp.StatusCode)
}
```

> **note**
> Your `OnError` handler will be called with `context.Canceled` for the losing hedge request. This is expected behavior and should not be counted as an error in your metrics. Filter it out explicitly as shown above.

---

## Combined with Circuit Breaker

Hedging and circuit breakers complement each other well. Hedging reduces latency from occasional slow requests. The circuit breaker prevents requests from being sent when the service is consistently failing.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://catalog.internal"),

        // Circuit breaker: open after 5 consecutive failures,
        // probe again after 10 seconds.
        relay.WithCircuitBreaker(relay.CircuitBreakerConfig{
            FailureThreshold:    5,
            RecoveryTimeout:     10 * time.Second,
            HalfOpenMaxRequests: 2,
        }),

        // Hedging: fire a speculative retry after 35ms.
        // Only fires when the circuit is closed (service is healthy).
        relay.WithHedging(35*time.Millisecond),

        // Overall timeout including all hedge attempts.
        relay.WithTimeout(500*time.Millisecond),
    )
    if err != nil {
        log.Fatal(err)
    }

    // The circuit breaker prevents requests from going to a failing service.
    // When the service is healthy, hedging reduces tail latency.
    for i := 0; i < 5; i++ {
        resp, err := client.Get(context.Background(), fmt.Sprintf("/items/%d", i), nil)
        if err != nil {
            log.Printf("request %d failed: %v", i, err)
            continue
        }
        resp.Body.Close()
        fmt.Printf("request %d: status=%d\n", i, resp.StatusCode)
    }
}
```

The interaction between hedging and the circuit breaker:

1. **Circuit closed (healthy)**: hedging is active, tail latency is reduced.
2. **Circuit opening (failures accumulating)**: hedges fire but also fail, contributing to the failure count.
3. **Circuit open (failing fast)**: hedging does not fire because requests are rejected before touching the network.
4. **Circuit half-open (probing)**: a single probe request is sent. If the hedge also fires and succeeds, the circuit may close faster.

> **tip**
> Set your circuit breaker's failure threshold to account for hedge cancellations. Since cancelled hedges may appear as errors in some configurations, ensure `context.Canceled` errors do not count toward the failure threshold.

---

## Summary

| Concern | Recommendation |
|---|---|
| Enable hedging | `WithHedging(after)` where `after` is near p95 latency |
| Limit parallel attempts | `WithHedgeMaxAttempts(2)` is usually enough |
| Safe methods only | GET, HEAD, OPTIONS - never POST, DELETE, PATCH |
| Handle cancellations | Filter `context.Canceled` in `OnError` |
| Monitor extra traffic | Expect up to ~5% extra requests when after=p95 |
| Combine with circuit breaker | Reduces both tail latency and cascading failures |
