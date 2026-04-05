# Bulkhead Isolation

The bulkhead pattern isolates sections of your application from each other so that a failure in one area cannot bring down the entire system. Named after the watertight compartments in a ship's hull, a bulkhead limits the number of concurrent requests that can be in-flight to a particular downstream service at any given time.

When a downstream service slows down, goroutines pile up waiting for responses. Without isolation, that backup can exhaust shared resources - thread pools, connection pools, or memory - and cascade into an application-wide outage. With a bulkhead, once the concurrent limit is reached, new requests are immediately rejected with `ErrBulkheadFull` rather than queuing indefinitely.

---

## WithMaxConcurrentRequests

```go
func WithMaxConcurrentRequests(n int) Option
```

`WithMaxConcurrentRequests` configures the maximum number of concurrent in-flight HTTP requests allowed through this client. Requests that arrive when `n` slots are already occupied are rejected immediately with `ErrBulkheadFull`.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    // Allow at most 20 concurrent requests to the payments service
    client, err := relay.New(
        relay.WithBaseURL("https://payments.internal"),
        relay.WithMaxConcurrentRequests(20),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/v1/charge", nil)
    if err != nil {
        log.Printf("request failed: %v", err)
        return
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

Setting `n` to `0` disables the bulkhead entirely, which is the default behavior. Any positive integer enables it.

---

## ErrBulkheadFull

```go
var ErrBulkheadFull = errors.New("relay: bulkhead is full, request rejected")
```

When the bulkhead limit is reached, `relay` returns `ErrBulkheadFull` immediately without making a network call. You should handle this error explicitly and apply appropriate backpressure or fallback logic.

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func fetchUserProfile(ctx context.Context, client *relay.Client, userID string) error {
    resp, err := client.Get(ctx, "/users/"+userID, nil)
    if err != nil {
        if errors.Is(err, relay.ErrBulkheadFull) {
            // Apply backpressure - return a 503 to the caller, serve from cache,
            // or enqueue for later processing.
            fmt.Println("bulkhead full: shedding load for user", userID)
            return fmt.Errorf("service temporarily unavailable: %w", err)
        }
        return fmt.Errorf("request failed: %w", err)
    }
    defer resp.Body.Close()
    return nil
}

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://profiles.internal"),
        relay.WithMaxConcurrentRequests(10),
    )
    if err != nil {
        log.Fatal(err)
    }

    if err := fetchUserProfile(context.Background(), client, "u-12345"); err != nil {
        log.Println(err)
    }
}
```

> **note**
> `ErrBulkheadFull` is never retried by the built-in retry middleware. It signals a local resource constraint, not a transient server error. Retrying would only deepen the pressure on an already saturated system.

---

## Why Bulkheads Matter: Preventing Cascade Failures

Consider a microservice that calls three downstream APIs: inventory, pricing, and recommendations. In a typical HTTP client with no isolation, all three share the same goroutine budget. If the recommendations service starts timing out at 30 seconds, goroutines accumulate waiting for it. Once enough goroutines pile up, even fast calls to inventory and pricing stall - because the Go runtime is spending time context-switching among hundreds of blocked goroutines, or because shared connection pools are exhausted.

```
Without bulkheads:
    Goroutines: [inv][inv][price][price][rec][rec][rec][rec][rec][rec][rec][rec]...
                                              ^ recommendations slowdown infects everyone

With bulkheads (limit=5 per service):
    Inventory:      [inv][inv]          -> still healthy, 0/5 slots used
    Pricing:        [price][price]      -> still healthy, 0/5 slots used
    Recommendations:[rec][rec][rec][rec][rec] -> FULL, rejects new requests immediately
                     ^ cascade is contained
```

The bulkhead ensures that a slowdown in one downstream service has zero impact on the throughput of requests to healthy services.

---

## Sizing the Bulkhead Correctly

Choosing the right concurrency limit requires understanding your service's capacity and your latency requirements. Use this formula as a starting point:

```
concurrency_limit = throughput_RPS * expected_p99_latency_seconds * safety_factor
```

For example:
- Throughput: 500 requests/second to the downstream service
- Expected p99 latency: 200ms (0.2 seconds)
- Safety factor: 1.5 (for burst headroom)

```
concurrency_limit = 500 * 0.2 * 1.5 = 150
```

```go
package main

import (
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func newInventoryClient() (*relay.Client, error) {
    // Inventory service: 500 RPS target, p99 = 200ms, safety factor 1.5
    const (
        targetRPS     = 500
        p99LatencySec = 0.2
        safetyFactor  = 1.5
        bulkheadSize  = int(targetRPS * p99LatencySec * safetyFactor) // 150
    )

    return relay.New(
        relay.WithBaseURL("https://inventory.internal"),
        relay.WithMaxConcurrentRequests(bulkheadSize),
        relay.WithTimeout(2*time.Second),
    )
}

func main() {
    client, err := newInventoryClient()
    if err != nil {
        log.Fatal(err)
    }
    _ = client
    log.Println("inventory client ready")
}
```

> **tip**
> Start conservative (lower limit) and increase based on observed metrics. It is far safer to shed load early than to allow unbounded queuing. Monitor `ErrBulkheadFull` rates - if they are consistently above 1% of total requests, the limit may need to be increased or the downstream service needs to be scaled.

---

## Goroutine Pool Pattern with Bulkhead

The bulkhead integrates naturally with a goroutine worker pool. In this pattern, you launch a bounded number of workers and use the bulkhead as a second layer of defense.

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log"
    "sync"
    "time"

    "github.com/jhonsferg/relay"
)

type Job struct {
    UserID string
}

type Result struct {
    UserID string
    Err    error
}

func processJobs(ctx context.Context, client *relay.Client, jobs []Job) []Result {
    const workerCount = 25

    jobCh := make(chan Job, len(jobs))
    for _, j := range jobs {
        jobCh <- j
    }
    close(jobCh)

    results := make([]Result, 0, len(jobs))
    var mu sync.Mutex
    var wg sync.WaitGroup

    for i := 0; i < workerCount; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for job := range jobCh {
                resp, err := client.Get(ctx, "/users/"+job.UserID, nil)
                if err != nil {
                    if errors.Is(err, relay.ErrBulkheadFull) {
                        // Re-queue or record as shed
                        mu.Lock()
                        results = append(results, Result{UserID: job.UserID, Err: err})
                        mu.Unlock()
                        continue
                    }
                    mu.Lock()
                    results = append(results, Result{UserID: job.UserID, Err: err})
                    mu.Unlock()
                    continue
                }
                resp.Body.Close()
                mu.Lock()
                results = append(results, Result{UserID: job.UserID})
                mu.Unlock()
            }
        }()
    }

    wg.Wait()
    return results
}

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://users.internal"),
        relay.WithMaxConcurrentRequests(20),
        relay.WithTimeout(3*time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }

    jobs := []Job{
        {UserID: "u-001"},
        {UserID: "u-002"},
        {UserID: "u-003"},
    }

    results := processJobs(context.Background(), client, jobs)
    for _, r := range results {
        if r.Err != nil {
            fmt.Printf("FAILED %s: %v\n", r.UserID, r.Err)
        } else {
            fmt.Printf("OK     %s\n", r.UserID)
        }
    }
}
```

The worker pool limits goroutine count at the application layer, while the bulkhead limits concurrency at the HTTP transport layer. Together they prevent both goroutine explosion and connection saturation.

---

## Monitoring Bulkhead Rejections via OnError Hook

Use the `OnError` hook to emit metrics whenever the bulkhead rejects a request. This gives you visibility into load-shedding events in your observability platform.

```go
package main

import (
    "context"
    "errors"
    "log"
    "sync/atomic"
    "time"

    "github.com/jhonsferg/relay"
)

var bulkheadRejections int64

func incrementBulkheadCounter() {
    atomic.AddInt64(&bulkheadRejections, 1)
}

func getBulkheadRejections() int64 {
    return atomic.LoadInt64(&bulkheadRejections)
}

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://orders.internal"),
        relay.WithMaxConcurrentRequests(30),
        relay.WithTimeout(2*time.Second),
        relay.OnError(func(ctx context.Context, err error) {
            if errors.Is(err, relay.ErrBulkheadFull) {
                incrementBulkheadCounter()
                log.Printf("bulkhead_rejection total=%d", getBulkheadRejections())
                // In production, emit to Prometheus, Datadog, etc.:
                // metrics.Counter("relay.bulkhead.rejected").Inc()
            }
        }),
    )
    if err != nil {
        log.Fatal(err)
    }

    _, err = client.Get(context.Background(), "/orders", nil)
    if err != nil {
        log.Println("request error:", err)
    }

    log.Printf("total bulkhead rejections: %d", getBulkheadRejections())
}
```

> **tip**
> Plot bulkhead rejections as a rate alongside your downstream service's error rate and latency. A rising rejection rate that correlates with downstream latency is a clear signal the downstream service needs attention - or the bulkhead limit needs to be revised.

---

## Example: Multiple Downstream APIs with Independent Bulkheads

In real services, you call multiple downstream APIs. Each should have its own bulkhead sized to that service's characteristics.

```go
package main

import (
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "log"
    "sync"
    "time"

    "github.com/jhonsferg/relay"
)

// ServiceClients holds one relay client per downstream service,
// each with its own bulkhead configuration.
type ServiceClients struct {
    Inventory       *relay.Client
    Pricing         *relay.Client
    Recommendations *relay.Client
}

func NewServiceClients() (*ServiceClients, error) {
    // Inventory: high-throughput, fast p99 - larger bulkhead
    inventory, err := relay.New(
        relay.WithBaseURL("https://inventory.internal"),
        relay.WithMaxConcurrentRequests(100),
        relay.WithTimeout(500*time.Millisecond),
    )
    if err != nil {
        return nil, fmt.Errorf("inventory client: %w", err)
    }

    // Pricing: medium throughput, medium latency
    pricing, err := relay.New(
        relay.WithBaseURL("https://pricing.internal"),
        relay.WithMaxConcurrentRequests(50),
        relay.WithTimeout(1*time.Second),
    )
    if err != nil {
        return nil, fmt.Errorf("pricing client: %w", err)
    }

    // Recommendations: low priority, can be slow - tight bulkhead
    // so slowdowns here never affect inventory or pricing
    recommendations, err := relay.New(
        relay.WithBaseURL("https://recommendations.internal"),
        relay.WithMaxConcurrentRequests(10),
        relay.WithTimeout(2*time.Second),
    )
    if err != nil {
        return nil, fmt.Errorf("recommendations client: %w", err)
    }

    return &ServiceClients{
        Inventory:       inventory,
        Pricing:         pricing,
        Recommendations: recommendations,
    }, nil
}

type ProductPage struct {
    Stock           int     `json:"stock"`
    Price           float64 `json:"price"`
    Recommendations []int   `json:"recommendations"`
}

func (sc *ServiceClients) BuildProductPage(ctx context.Context, productID string) (*ProductPage, error) {
    page := &ProductPage{}
    var mu sync.Mutex
    var wg sync.WaitGroup
    errs := make([]error, 0)

    // Fetch inventory - critical path
    wg.Add(1)
    go func() {
        defer wg.Done()
        resp, err := sc.Inventory.Get(ctx, "/products/"+productID+"/stock", nil)
        if err != nil {
            mu.Lock()
            errs = append(errs, fmt.Errorf("inventory: %w", err))
            mu.Unlock()
            return
        }
        defer resp.Body.Close()
        var data struct {
            Stock int `json:"stock"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
            mu.Lock()
            page.Stock = data.Stock
            mu.Unlock()
        }
    }()

    // Fetch pricing - critical path
    wg.Add(1)
    go func() {
        defer wg.Done()
        resp, err := sc.Pricing.Get(ctx, "/products/"+productID+"/price", nil)
        if err != nil {
            mu.Lock()
            errs = append(errs, fmt.Errorf("pricing: %w", err))
            mu.Unlock()
            return
        }
        defer resp.Body.Close()
        var data struct {
            Price float64 `json:"price"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
            mu.Lock()
            page.Price = data.Price
            mu.Unlock()
        }
    }()

    // Fetch recommendations - best-effort, non-critical
    wg.Add(1)
    go func() {
        defer wg.Done()
        resp, err := sc.Recommendations.Get(ctx, "/products/"+productID+"/related", nil)
        if err != nil {
            if errors.Is(err, relay.ErrBulkheadFull) {
                // Recommendations are non-critical - swallow and continue
                log.Println("recommendations bulkhead full, skipping")
                return
            }
            // Also non-fatal for other errors
            log.Printf("recommendations error: %v", err)
            return
        }
        defer resp.Body.Close()
        var data struct {
            ProductIDs []int `json:"product_ids"`
        }
        if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
            mu.Lock()
            page.Recommendations = data.ProductIDs
            mu.Unlock()
        }
    }()

    wg.Wait()

    // Critical path errors are fatal
    for _, err := range errs {
        if err != nil {
            return nil, err
        }
    }
    return page, nil
}

func main() {
    clients, err := NewServiceClients()
    if err != nil {
        log.Fatal(err)
    }

    page, err := clients.BuildProductPage(context.Background(), "prod-789")
    if err != nil {
        log.Fatal("failed to build product page:", err)
    }

    fmt.Printf("Product page: stock=%d price=%.2f recommendations=%v\n",
        page.Stock, page.Price, page.Recommendations)
}
```

> **warning**
> Never share a single `relay.Client` instance across multiple logical services just to save on allocation. The bulkhead is per-client. A shared client means a shared bulkhead, which defeats the entire purpose of isolation.

This pattern ensures that even if the recommendations service degrades completely (bulkhead always full), the product page still renders with correct inventory and pricing data.

---

## Summary

| Concern | Recommendation |
|---|---|
| Limit concurrency | `WithMaxConcurrentRequests(n)` |
| Handle rejections | Check `errors.Is(err, relay.ErrBulkheadFull)` |
| Size the limit | Use `RPS * p99_latency * safety_factor` |
| Monitor rejections | Use `OnError` hook to emit metrics |
| Per-service isolation | One `relay.Client` per downstream service |
| Non-critical services | Swallow `ErrBulkheadFull`, return degraded data |
