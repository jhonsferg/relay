# Transport Adapters

`relay` uses Go's `http.RoundTripper` interface as its transport abstraction. By replacing or wrapping the transport, you can route requests over Unix sockets, custom schemes, alternative HTTP implementations (HTTP/2, HTTP/3), or inject cross-cutting behavior like request signing, logging, or metrics at the transport level.

The transport layer sits below the `relay` middleware stack (retries, circuit breakers, timeouts). Transport adapters affect raw network I/O, while middleware affects the logical request lifecycle.

---

## WithTransport

```go
func WithTransport(transport http.RoundTripper) Option
```

`WithTransport` replaces the default `http.Transport` entirely. Every request made by the client will use this transport, regardless of the URL scheme.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    // Custom transport with aggressive connection pool settings
    transport := &http.Transport{
        MaxIdleConns:        200,
        MaxIdleConnsPerHost: 100,
        MaxConnsPerHost:     200,
        IdleConnTimeout:     90 * time.Second,
        TLSHandshakeTimeout: 5 * time.Second,
        DisableCompression:  false,
    }

    client, err := relay.New(
        relay.WithBaseURL("https://high-throughput-api.internal"),
        relay.WithTransport(transport),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/data", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

> **note**
> The default `http.Transport` is shared across all clients in the process unless you explicitly provide your own. For high-load services with multiple downstream targets, creating a dedicated transport per `relay.Client` (or per target) is recommended to prevent connection pool contention.

---

## WithTransportAdapter

```go
func WithTransportAdapter(scheme string, transport http.RoundTripper) Option
```

`WithTransportAdapter` registers a transport for a specific URL scheme. When a request is made to a URL with that scheme, the registered transport is used. All other schemes fall back to the default transport.

This is how `relay` supports routing to Unix sockets via a custom scheme, or to internal mesh endpoints via a private scheme.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net"
    "net/http"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    // Register a transport adapter for the "unix" scheme that dials over
    // a Unix domain socket instead of TCP.
    unixSocketPath := "/var/run/myservice/api.sock"

    unixTransport := &http.Transport{
        DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
            dialer := &net.Dialer{Timeout: 2 * time.Second}
            return dialer.DialContext(ctx, "unix", unixSocketPath)
        },
        MaxIdleConns:    10,
        IdleConnTimeout: 30 * time.Second,
    }

    client, err := relay.New(
        relay.WithBaseURL("http://localhost"), // base, overridden by full URL
        relay.WithTransportAdapter("unix", unixTransport),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Requests to unix:// go over the socket; https:// goes over TCP
    resp, err := client.Get(context.Background(), "unix://localhost/api/status", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

---

## WithTransportMiddleware

```go
func WithTransportMiddleware(fn func(http.RoundTripper) http.RoundTripper) Option
```

`WithTransportMiddleware` wraps the transport (whether default or custom) with a middleware function. Multiple calls to `WithTransportMiddleware` chain the wrappers in order: the first call wraps the outermost layer, the last call wraps the innermost (closest to the network).

This is the right place for cross-cutting transport concerns: adding headers to every raw request, tracing TCP-level events, or enforcing connection-level policies.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "time"

    "github.com/jhonsferg/relay"
)

// loggingTransport wraps a RoundTripper and logs every request and response.
func loggingMiddleware(next http.RoundTripper) http.RoundTripper {
    return relay.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
        start := time.Now()
        log.Printf("[transport] --> %s %s", req.Method, req.URL)
        resp, err := next.RoundTrip(req)
        elapsed := time.Since(start)
        if err != nil {
            log.Printf("[transport] <-- ERROR %s %s (%s): %v", req.Method, req.URL, elapsed, err)
            return nil, err
        }
        log.Printf("[transport] <-- %d %s %s (%s)", resp.StatusCode, req.Method, req.URL, elapsed)
        return resp, nil
    })
}

// requestIDMiddleware injects a unique request ID into every raw request.
func requestIDMiddleware(next http.RoundTripper) http.RoundTripper {
    return relay.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
        // Clone the request to avoid mutating the original
        cloned := req.Clone(req.Context())
        cloned.Header.Set("X-Transport-ID", fmt.Sprintf("t-%d", time.Now().UnixNano()))
        return next.RoundTrip(cloned)
    })
}

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTransportMiddleware(loggingMiddleware),
        relay.WithTransportMiddleware(requestIDMiddleware),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/orders", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

---

## Unix Socket Transport Example

Unix domain sockets are faster than TCP for inter-process communication on the same host because they skip the network stack entirely. They are commonly used by local sidecars (e.g., Envoy), agent APIs (e.g., Docker, containerd), and local daemons.

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "net"
    "net/http"
    "time"

    "github.com/jhonsferg/relay"
)

// dockerSocketTransport dials the Docker daemon's Unix socket.
func dockerSocketTransport() http.RoundTripper {
    return &http.Transport{
        DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
            return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
        },
        MaxIdleConns:    5,
        IdleConnTimeout: 30 * time.Second,
    }
}

type DockerVersion struct {
    Version       string `json:"Version"`
    APIVersion    string `json:"ApiVersion"`
    GoVersion     string `json:"GoVersion"`
    Os            string `json:"Os"`
    Arch          string `json:"Arch"`
}

func main() {
    // The host in the URL is ignored when dialing via Unix socket,
    // but it must be present for the HTTP/1.1 Host header.
    client, err := relay.New(
        relay.WithBaseURL("http://docker"),
        relay.WithTransportAdapter("http", dockerSocketTransport()),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/v1.43/version", nil)
    if err != nil {
        log.Fatal("docker socket error:", err)
    }
    defer resp.Body.Close()

    var version DockerVersion
    if err := json.NewDecoder(resp.Body).Decode(&version); err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Docker %s (API %s) on %s/%s\n",
        version.Version, version.APIVersion, version.Os, version.Arch)
}
```

---

## Custom Scheme Example

Custom URL schemes let you route requests to internal registries or mesh endpoints using logical names rather than hardcoded IP addresses. This is useful for service meshes and internal routing layers.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net"
    "net/http"
    "time"

    "github.com/jhonsferg/relay"
)

// internalSchemeTransport resolves "internal://service-name/path" to
// internal service addresses via a private registry or DNS zone.
func internalSchemeTransport(registry map[string]string) http.RoundTripper {
    return &http.Transport{
        DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
            // addr will be "service-name:80" or similar
            host, port, err := net.SplitHostPort(addr)
            if err != nil {
                return nil, fmt.Errorf("invalid addr %q: %w", addr, err)
            }
            // Resolve the logical service name to a real address
            resolved, ok := registry[host]
            if !ok {
                resolved = host + ".internal.example.com"
            }
            realAddr := net.JoinHostPort(resolved, port)
            dialer := &net.Dialer{Timeout: 3 * time.Second}
            return dialer.DialContext(ctx, "tcp", realAddr)
        },
        TLSHandshakeTimeout: 5 * time.Second,
        MaxIdleConns:        50,
        IdleConnTimeout:     60 * time.Second,
    }
}

func main() {
    // Registry maps logical service names to real endpoints
    registry := map[string]string{
        "orders":    "10.0.1.42",
        "inventory": "10.0.1.55",
        "pricing":   "10.0.1.60",
    }

    client, err := relay.New(
        relay.WithBaseURL("internal://orders"),
        relay.WithTransportAdapter("internal", internalSchemeTransport(registry)),
    )
    if err != nil {
        log.Fatal(err)
    }

    // URL uses "internal://" scheme - routed through the custom transport
    resp, err := client.Get(context.Background(), "internal://orders/api/v1/status", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("orders service status:", resp.StatusCode)
}
```

---

## HTTP/3 via ext/http3

`relay` provides HTTP/3 support through the `ext/http3` extension package. HTTP/3 runs over QUIC (UDP) instead of TCP, which reduces connection establishment latency and improves performance on lossy networks.

See the [HTTP/3 extension documentation](../extensions/http3.md) for full setup instructions, including the required `github.com/quic-go/quic-go` dependency.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
    relayhttp3 "github.com/jhonsferg/relay/ext/http3"
)

func main() {
    // Create an HTTP/3 transport using the ext/http3 package.
    // This requires quic-go to be available.
    h3Transport, err := relayhttp3.NewTransport(relayhttp3.TransportConfig{
        TLSConfig: nil, // Use default system CA pool
    })
    if err != nil {
        log.Fatal("create HTTP/3 transport:", err)
    }
    defer h3Transport.Close()

    client, err := relay.New(
        relay.WithBaseURL("https://cloudflare.com"),
        // Register HTTP/3 transport for HTTPS scheme.
        // Falls back to standard HTTPS if QUIC is not supported by the server.
        relay.WithTransportAdapter("https", h3Transport),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Get(context.Background(), "/", nil)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("HTTP/3 response:", resp.StatusCode, resp.Proto)
}
```

> **tip**
> HTTP/3 is most beneficial on high-latency or lossy connections (mobile, international traffic). For low-latency datacenter traffic, the improvement over HTTP/2 is typically negligible. Profile before adopting HTTP/3 in your internal infrastructure.

---

## Combining Multiple Transport Adapters

You can register transport adapters for multiple schemes on the same client. This allows a single client to transparently route to different backends depending on the URL scheme.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net"
    "net/http"
    "time"

    "github.com/jhonsferg/relay"
)

func unixTransportFor(socketPath string) http.RoundTripper {
    return &http.Transport{
        DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
            return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", socketPath)
        },
        MaxIdleConns: 5,
    }
}

func internalMeshTransport() http.RoundTripper {
    return &http.Transport{
        DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
            host, port, _ := net.SplitHostPort(addr)
            // Append mesh DNS suffix
            meshAddr := net.JoinHostPort(host+".mesh.local", port)
            return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", meshAddr)
        },
        MaxIdleConns:    100,
        IdleConnTimeout: 90 * time.Second,
    }
}

func main() {
    client, err := relay.New(
        // Default base URL - overridden per-request with full URLs
        relay.WithBaseURL("https://api.example.com"),

        // "unix" scheme - dials a local Unix socket
        relay.WithTransportAdapter("unix", unixTransportFor("/var/run/agent/api.sock")),

        // "mesh" scheme - resolves service names through internal mesh
        relay.WithTransportAdapter("mesh", internalMeshTransport()),

        // "https" uses the default transport (no adapter registration needed)

        // Wrap all transports with logging
        relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
            return relay.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
                log.Printf("transport: %s %s", req.Method, req.URL)
                return next.RoundTrip(req)
            })
        }),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Each request is routed by its URL scheme
    requests := []string{
        "unix://agent/v1/status",
        "mesh://inventory-service/products",
        "https://api.example.com/public/data",
    }

    for _, url := range requests {
        resp, err := client.Get(context.Background(), url, nil)
        if err != nil {
            log.Printf("ERROR %s: %v", url, err)
            continue
        }
        resp.Body.Close()
        fmt.Printf("OK %s: %d\n", url, resp.StatusCode)
    }
}
```

> **note**
> Transport middlewares registered with `WithTransportMiddleware` apply to **all** scheme adapters, not just the default transport. This makes them suitable for universal cross-cutting concerns like distributed tracing or connection-level metrics.

---

## Summary

| Goal | API |
|---|---|
| Replace default transport | `WithTransport(transport)` |
| Route by URL scheme | `WithTransportAdapter(scheme, transport)` |
| Wrap all transports | `WithTransportMiddleware(fn)` |
| Unix socket communication | Custom `DialContext` pointing to `.sock` file |
| Custom internal routing | Custom scheme + registry-based `DialContext` |
| HTTP/3 (QUIC) | `ext/http3` extension - see [extensions/http3](../extensions/http3.md) |
| Multiple scheme routing | Multiple `WithTransportAdapter` calls on one client |
