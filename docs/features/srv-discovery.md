# SRV Discovery

`SRVResolver` adds automatic DNS SRV record-based service discovery to relay clients. Before each request the resolver queries the DNS SRV record for your service and rewrites the request's `Host` to the selected backend. This eliminates the need for a separate load-balancer for internal service-to-service traffic.

---

## Usage

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
    // Resolve _http._tcp.payments.internal, round-robin, cache results for 30s.
    resolver := relay.NewSRVResolver(
        "http",                        // service
        "tcp",                         // protocol
        "payments.internal",           // domain
        "http",                        // URL scheme
        relay.WithSRVBalancer(relay.SRVRoundRobin),
        relay.WithSRVTTL(30*time.Second),
    )

    client, err := relay.New(
        relay.WithBaseURL("http://payments.internal"),
        relay.WithSRVDiscovery(resolver),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Each request is transparently routed to an SRV-resolved backend.
    ctx := context.Background()
    resp, err := client.Get(ctx, "/charge", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

---

## API Reference

### `NewSRVResolver`

```go
func NewSRVResolver(service, proto, name, scheme string, opts ...SRVOption) *SRVResolver
```

Creates an SRV resolver. DNS queries take the form `_<service>._<proto>.<name>`.

| Parameter | Example | Description |
|-----------|---------|-------------|
| `service` | `"http"`, `"grpc"` | SRV service label |
| `proto` | `"tcp"`, `"udp"` | SRV protocol label |
| `name` | `"payments.internal"` | Domain to query |
| `scheme` | `"http"`, `"https"` | HTTP scheme for built URLs |

### `WithSRVDiscovery`

```go
func WithSRVDiscovery(resolver *SRVResolver) Option
```

Attaches the resolver to a relay client as a transport middleware.

### `Resolve`

```go
func (r *SRVResolver) Resolve(ctx context.Context) (string, error)
```

Performs (or returns a cached) SRV lookup and returns the selected `"host:port"` string. Called automatically by the transport middleware; you can also call it directly.

---

## Options

### `WithSRVBalancer`

```go
func WithSRVBalancer(b SRVBalancer) SRVOption
```

Sets the load balancing strategy. Available strategies:

| Constant | Description |
|----------|-------------|
| `SRVRoundRobin` (default) | Rotate through targets in order |
| `SRVRandom` | Pick a random target each time |
| `SRVPriority` | Always use the target with the lowest priority number (highest precedence) |

### `WithSRVTTL`

```go
func WithSRVTTL(d time.Duration) SRVOption
```

Caches DNS SRV lookup results for `d`. When `d` is zero (default) every request triggers a fresh DNS lookup.

---

## Notes

- The resolver rewrites both `req.URL.Host` and the `Host` header on each request.
- The `baseURL` you pass to `relay.New` still controls the HTTP path and default host header; only the network-level host is overwritten per request.
- Set a non-zero TTL in production to avoid per-request DNS latency; a value of 10–60 seconds is typical.
- `SRVPriority` sorts records ascending by `Priority` field (lower number = higher priority) and always picks the first. For weight-based selection within a priority group, use `SRVRandom`.
