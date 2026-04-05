# DNS Caching

In a typical Go HTTP client, DNS resolution happens on every new TCP connection. The OS DNS resolver (or Go's own pure-Go resolver) queries the configured nameservers, which involves at least one round-trip before the actual HTTP connection can begin. In environments with many short-lived connections or high-frequency requests to many distinct hostnames, DNS resolution overhead adds measurable latency.

`relay`'s built-in DNS cache stores resolved IP addresses in memory for a configurable TTL. Subsequent connections to the same hostname skip DNS resolution entirely and dial the cached IP directly, reducing connection establishment latency.

---

## WithDNSCache

```go
func WithDNSCache(ttl time.Duration) Option
```

`WithDNSCache` enables in-process DNS caching for the client. Resolved IP addresses are stored in a concurrent-safe map. Entries expire after `ttl` and are refreshed on the next request to that hostname after expiry.

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
    // Cache DNS results for 30 seconds.
    // Requests to the same hostname within 30s reuse the cached IP.
    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithDNSCache(30*time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }

    // First request: DNS resolution happens, result is cached
    resp, err := client.Get(context.Background(), "/users", nil)
    if err != nil {
        log.Fatal(err)
    }
    resp.Body.Close()
    fmt.Println("first request (DNS resolved):", resp.StatusCode)

    // Second request: uses cached IP, no DNS query
    resp, err = client.Get(context.Background(), "/users/42", nil)
    if err != nil {
        log.Fatal(err)
    }
    resp.Body.Close()
    fmt.Println("second request (cached DNS):", resp.StatusCode)
}
```

> **note**
> DNS caching is applied at the `DialContext` level, before the TCP connection is established. It is separate from HTTP connection keep-alive. Even when HTTP connections are reused (keep-alive), the DNS cache provides a safety net for when those connections are dropped and re-established.

---

## How DNS Caching Works in relay

When `WithDNSCache(ttl)` is configured, `relay` installs a caching resolver that wraps the system's default `net.Resolver`. The resolution flow is:

1. A new TCP connection is needed for hostname `api.example.com`.
2. `relay`'s caching resolver checks its in-memory map for `api.example.com`.
3. **Cache hit**: the cached IP(s) are returned immediately. No DNS query is made.
4. **Cache miss or expired entry**: `net.Resolver.LookupIPAddr` is called to resolve the hostname. The result is stored in the cache with the configured TTL.
5. The TCP connection is established to the resolved IP.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "sync"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    // With a 60-second TTL, DNS is resolved at most once per minute per hostname.
    client, err := relay.New(
        relay.WithBaseURL("https://cdn.example.com"),
        relay.WithDNSCache(60*time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }

    var wg sync.WaitGroup
    for i := 0; i < 50; i++ {
        wg.Add(1)
        go func(n int) {
            defer wg.Done()
            resp, err := client.Get(context.Background(), fmt.Sprintf("/assets/image-%d.png", n), nil)
            if err != nil {
                log.Printf("request %d failed: %v", n, err)
                return
            }
            resp.Body.Close()
        }(i)
    }
    wg.Wait()

    // Despite 50 concurrent requests, DNS was resolved only once (for the first request).
    // All subsequent requests reused the cached IP.
    fmt.Println("50 requests completed with at most 1 DNS lookup")
}
```

---

## Default TTL and Override

`relay` does not have a built-in default TTL for DNS caching - you must explicitly specify the TTL when calling `WithDNSCache`. This design choice is intentional: DNS TTL requirements vary significantly between use cases, and silently choosing a default could cause subtle correctness issues.

Common TTL values and their trade-offs:

| TTL | Use case | Trade-off |
|---|---|---|
| 5s | Kubernetes services that scale quickly | Frequent DNS queries, fast failover |
| 30s | Standard microservice-to-microservice | Good balance of performance and freshness |
| 60s | Stable external APIs (Stripe, AWS) | Fewer queries, slightly slower failover |
| 300s | CDN edge endpoints (rarely change) | Very few queries, slow to detect IP changes |

```go
package main

import (
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    // Short TTL for a Kubernetes service that uses pod IP endpoints
    k8sClient, err := relay.New(
        relay.WithBaseURL("https://my-service.default.svc.cluster.local"),
        relay.WithDNSCache(5*time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Long TTL for a stable external API
    stripeClient, err := relay.New(
        relay.WithBaseURL("https://api.stripe.com"),
        relay.WithDNSCache(5*time.Minute),
    )
    if err != nil {
        log.Fatal(err)
    }

    log.Println("k8s client:", k8sClient)
    log.Println("stripe client:", stripeClient)
}
```

---

## Cache Invalidation

Cache entries expire automatically when their TTL elapses. The entry is removed lazily - it is not actively evicted when it expires, but it is treated as a miss the next time a connection to that hostname is requested.

You can also manually flush the entire DNS cache if you need to force fresh resolution immediately - for example, after receiving a webhook notification that a service's IP has changed:

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
        relay.WithBaseURL("https://partner.example.com"),
        relay.WithDNSCache(60*time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Normal request - DNS is cached for 60 seconds
    resp, err := client.Get(context.Background(), "/status", nil)
    if err != nil {
        log.Fatal(err)
    }
    resp.Body.Close()
    fmt.Println("initial request ok")

    // Suppose we received a notification that the partner service has
    // migrated to new IPs. Flush the cache to force re-resolution.
    client.FlushDNSCache()
    fmt.Println("DNS cache flushed")

    // This request will re-resolve DNS regardless of the TTL
    resp, err = client.Get(context.Background(), "/status", nil)
    if err != nil {
        log.Fatal(err)
    }
    resp.Body.Close()
    fmt.Println("post-flush request ok (fresh DNS):", resp.StatusCode)

    // You can also invalidate a single hostname
    client.InvalidateDNSEntry("partner.example.com")
    fmt.Println("specific entry invalidated")
}
```

> **note**
> `FlushDNSCache()` is safe to call from any goroutine. It is a synchronous, blocking call that clears the cache atomically. Requests in flight at the time of the flush are not affected - they already have a resolved IP from the dialing phase.

---

## Why DNS Caching Matters in Kubernetes

Kubernetes presents a particularly important case for DNS caching due to the interaction between the DNS TTL, the DNS server load, and connection keep-alive.

In a Kubernetes cluster, `CoreDNS` handles DNS resolution. The default TTL for in-cluster DNS records (ClusterIP services) is 5 seconds. If you have 100 pods each making 50 requests/second to a downstream service, and every request triggers a DNS lookup, you get:

```
100 pods * 50 req/s * 1 DNS lookup/req = 5,000 DNS queries/second to CoreDNS
```

This can saturate CoreDNS and cause intermittent `SERVFAIL` responses, which manifest as mysterious connection timeouts.

With DNS caching at a 30-second TTL:

```
100 pods * (1 DNS lookup / 30s) = ~3.3 DNS queries/second to CoreDNS
```

A 1,500x reduction in DNS load.

```go
package main

import (
    "context"
    "log"
    "sync"
    "time"

    "github.com/jhonsferg/relay"
)

func newKubernetesClient(serviceURL string) (*relay.Client, error) {
    return relay.New(
        relay.WithBaseURL(serviceURL),
        // 30-second TTL balances DNS freshness with CoreDNS load.
        // After a rolling deployment, new pod IPs are picked up within 30s.
        relay.WithDNSCache(30*time.Second),
        relay.WithTimeout(5*time.Second),
        relay.WithRetry(relay.RetryConfig{
            MaxAttempts: 3,
            Backoff:     relay.ExponentialBackoff(100*time.Millisecond, 2.0),
        }),
    )
}

func main() {
    client, err := newKubernetesClient("https://inventory.default.svc.cluster.local")
    if err != nil {
        log.Fatal(err)
    }

    // Simulate 100 concurrent requests
    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            resp, err := client.Get(context.Background(), "/health", nil)
            if err != nil {
                log.Printf("health check failed: %v", err)
                return
            }
            resp.Body.Close()
        }()
    }
    wg.Wait()
    log.Println("100 health checks completed with minimal DNS load")
}
```

> **tip**
> In Kubernetes, also consider using `ndots:5` optimization. By default, Go's resolver appends multiple search domain suffixes before resolving a short hostname. Appending the full FQDN (ending with `.`) bypasses search domain expansion and reduces DNS queries further: `inventory.default.svc.cluster.local.` instead of `inventory`.

---

## Performance Impact

The performance improvement from DNS caching is most visible in two scenarios:

**Scenario 1: High connection churn**
Services that establish many short-lived connections (HTTP/1.0 without keep-alive, microbursts of traffic) see the largest benefit because DNS is resolved for every new TCP connection.

**Scenario 2: Large fan-out**
A service that makes requests to many different hostnames (e.g., a gateway that proxies to 50 different microservices) benefits because DNS caching amortizes the resolver overhead across all calls to each hostname.

```go
package main

import (
    "context"
    "fmt"
    "sync"
    "time"
    "log"

    "github.com/jhonsferg/relay"
)

func benchmark(label string, client *relay.Client, requests int) {
    var wg sync.WaitGroup
    start := time.Now()
    for i := 0; i < requests; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            resp, err := client.Get(context.Background(), "/ping", nil)
            if err == nil {
                resp.Body.Close()
            }
        }()
    }
    wg.Wait()
    elapsed := time.Since(start)
    fmt.Printf("%-25s %d req in %s (%.0f req/s)\n",
        label, requests, elapsed, float64(requests)/elapsed.Seconds())
}

func main() {
    withCache, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithDNSCache(30*time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }

    withoutCache, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        // No DNS caching
    )
    if err != nil {
        log.Fatal(err)
    }

    benchmark("without DNS cache:", withoutCache, 500)
    benchmark("with DNS cache:   ", withCache, 500)
}
```

In a typical environment, DNS caching reduces mean request latency by 1ms to 5ms per request when the DNS resolver is local (same node), and by 5ms to 50ms when DNS queries cross a network boundary.

---

## Thread Safety

The DNS cache is fully thread-safe. All cache operations (read, write, expire, flush) are protected by an internal `sync.RWMutex`. Multiple goroutines can read from the cache concurrently without contention. Writes (on cache miss or TTL expiry) acquire an exclusive lock for the duration of the update.

The lock granularity is per-cache, not per-entry. For caches with many concurrent miss-and-refresh events (e.g., cold start of a large fan-out service), there is brief contention. In practice this is negligible because DNS resolutions for distinct hostnames complete in milliseconds.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "sync"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithDNSCache(30*time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Safe: all goroutines share the same client and its DNS cache.
    // Concurrent reads from the cache are non-blocking.
    // Writes (on cache miss) are serialized but brief.
    const goroutines = 200
    var wg sync.WaitGroup
    successCount := sync.Map{}

    for i := 0; i < goroutines; i++ {
        wg.Add(1)
        go func(n int) {
            defer wg.Done()
            resp, err := client.Get(context.Background(), fmt.Sprintf("/items/%d", n), nil)
            if err != nil {
                return
            }
            resp.Body.Close()
            successCount.Store(n, true)
        }(i)
    }

    wg.Wait()

    count := 0
    successCount.Range(func(_, _ interface{}) bool {
        count++
        return true
    })
    log.Printf("%d/%d requests succeeded with concurrent DNS cache access", count, goroutines)

    // Flush is also thread-safe - can be called from any goroutine
    go func() {
        time.Sleep(100 * time.Millisecond)
        client.FlushDNSCache()
        log.Println("cache flushed from background goroutine - safe")
    }()

    time.Sleep(200 * time.Millisecond)
    fmt.Println("all operations completed safely")
}
```

> **note**
> A single `relay.Client` instance should be shared across goroutines. Creating a new `relay.Client` per request defeats DNS caching (the cache is per-client) and also defeats HTTP connection keep-alive. Always create one client per logical downstream service and reuse it throughout the lifetime of your application.

---

## Summary

| Feature | API |
|---|---|
| Enable DNS caching | `WithDNSCache(ttl)` |
| Flush entire cache | `client.FlushDNSCache()` |
| Invalidate one entry | `client.InvalidateDNSEntry(hostname)` |
| Recommended TTL (Kubernetes) | 5s to 30s |
| Recommended TTL (external APIs) | 60s to 5m |
| Thread safety | Full - uses `sync.RWMutex` internally |
| Performance benefit | 1ms to 50ms per request (DNS RTT eliminated) |
| Kubernetes benefit | Reduces CoreDNS query load by 100x to 1000x |
