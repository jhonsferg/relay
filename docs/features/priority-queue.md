# Request Priority Queue

The priority queue allows requests to be ordered by urgency when the bulkhead is at capacity. Instead of rejecting overflow requests immediately with `ErrBulkheadFull`, they wait in a max-heap ordered queue -- higher-priority requests are dequeued first, and requests at the same priority level are served in arrival order (FIFO).

This is useful when a mix of critical and background traffic shares a single client: a health-check or authentication request should never be delayed behind a batch job.

---

## WithPriorityQueue

```go
func WithPriorityQueue() Option
```

`WithPriorityQueue` enables priority-aware dequeuing. It **must** be combined with [`WithMaxConcurrentRequests`](bulkhead.md) -- the bulkhead provides the concurrency ceiling, and the priority queue governs the order in which waiting requests are admitted.

```go
package main

import (
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithMaxConcurrentRequests(10),
        relay.WithPriorityQueue(),
    )
    if err != nil {
        log.Fatal(err)
    }
    _ = client
}
```

When `WithPriorityQueue` is active, requests that arrive whilst the bulkhead is full are queued rather than rejected. They are released in priority order as slots become free. Context cancellation or deadline expiry removes a request from the queue cleanly.

---

## Priority Constants

```go
type Priority int

const (
    PriorityLow      Priority = 0
    PriorityNormal   Priority = 50
    PriorityHigh     Priority = 100
    PriorityCritical Priority = 200
)
```

| Constant | Value | Intended use |
|---|---|---|
| `PriorityLow` | 0 | Background jobs, pre-fetching, analytics |
| `PriorityNormal` | 50 | Default for typical application requests |
| `PriorityHigh` | 100 | User-initiated actions, important reads |
| `PriorityCritical` | 200 | Health checks, authentication, token refresh |

Higher values are dequeued first. Values are plain integers so you can define intermediate levels if needed.

---

## WithPriority

```go
func (r *Request) WithPriority(p Priority) *Request
```

`WithPriority` attaches a priority level to a single request. When the client has `WithPriorityQueue` enabled, this value determines the request's position in the queue. Without `WithPriorityQueue`, the field is ignored.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithMaxConcurrentRequests(5),
        relay.WithPriorityQueue(),
    )
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()

    // Health check: always runs ahead of ordinary traffic
    healthReq := client.NewRequest("GET", "/healthz").
        WithPriority(relay.PriorityCritical)

    resp, err := client.Execute(healthReq.WithContext(ctx))
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("health:", resp.StatusCode)

    // Bulk data sync: low urgency, yields to everything else
    syncReq := client.NewRequest("POST", "/sync").
        WithPriority(relay.PriorityLow)

    resp, err = client.Execute(syncReq.WithContext(ctx))
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("sync:", resp.StatusCode)
}
```

---

## Integration with the Bulkhead

The priority queue sits between incoming requests and the bulkhead semaphore:

```
Request arrives
      │
      ▼
Bulkhead slot free? ──yes──▶ Execute immediately
      │
      no
      ▼
Enqueue with priority
      │
      ▼
Wait for slot (or ctx cancel)
      │
      ▼
Dequeued in priority order ──▶ Execute
```

The queue is a max-heap. When multiple requests are waiting, the one with the highest `Priority` value is selected next. Within the same priority level, the earliest-arrived request wins (FIFO).

```go
package main

import (
    "context"
    "fmt"
    "log"
    "sync"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithMaxConcurrentRequests(2), // tight limit to demonstrate ordering
        relay.WithPriorityQueue(),
    )
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()
    var wg sync.WaitGroup

    send := func(name string, p relay.Priority) {
        wg.Add(1)
        go func() {
            defer wg.Done()
            req := client.NewRequest("GET", "/work").WithPriority(p)
            resp, err := client.Execute(req.WithContext(ctx))
            if err != nil {
                fmt.Printf("%s: error: %v\n", name, err)
                return
            }
            fmt.Printf("%s (%v): %d\n", name, p, resp.StatusCode)
        }()
    }

    send("batch-export",    relay.PriorityLow)
    send("product-fetch",   relay.PriorityNormal)
    send("user-action",     relay.PriorityHigh)
    send("auth-refresh",    relay.PriorityCritical)

    wg.Wait()
}
```

> **note**
> Setting `WithPriorityQueue` without `WithMaxConcurrentRequests` has no effect: if there is no bulkhead, requests never queue and priority is irrelevant.

---

## Context Cancellation and Deadlines

A queued request that has its context cancelled or deadline exceeded is removed from the queue without executing. The caller receives the standard `context.Canceled` or `context.DeadlineExceeded` error.

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithMaxConcurrentRequests(1),
        relay.WithPriorityQueue(),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Short deadline -- request may be cancelled whilst queued
    ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
    defer cancel()

    req := client.NewRequest("GET", "/slow-resource").
        WithPriority(relay.PriorityLow)

    _, err = client.Execute(req.WithContext(ctx))
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            fmt.Println("request timed out in queue -- try again or escalate priority")
        } else {
            log.Fatal(err)
        }
    }
}
```

---

## Summary

| Concern | Recommendation |
|---|---|
| Enable priority queue | `WithPriorityQueue()` + `WithMaxConcurrentRequests(n)` |
| Mark a request | `.WithPriority(relay.PriorityCritical)` |
| Default priority | `PriorityNormal` (50) |
| Background / batch work | `PriorityLow` (0) |
| Health checks / auth | `PriorityCritical` (200) |
| Queue-position on cancel | Removed cleanly, caller gets `context.Canceled` |
