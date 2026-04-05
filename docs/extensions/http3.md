# HTTP/3 Extension

The `http3` extension enables HTTP/3 (QUIC) transport for relay clients. HTTP/3 runs over UDP using the QUIC protocol and provides lower latency connection establishment (0-RTT), improved multiplexing without head-of-line blocking, and better performance on lossy networks compared to HTTP/1.1 and HTTP/2.

> **note**
> HTTP/3 requires a TLS connection. Plain HTTP (`http://`) over QUIC is not supported by this extension or by the HTTP/3 specification.

## Installation

```bash
go get github.com/jhonsferg/relay/ext/http3
```

The extension depends on `quic-go`:

```bash
go get github.com/quic-go/quic-go
```

## Import

```go
import relayhttp3 "github.com/jhonsferg/relay/ext/http3"
```

## Quick Start

```go
package main

import (
    "context"
    "io"
    "log"

    "github.com/jhonsferg/relay"
    relayhttp3 "github.com/jhonsferg/relay/ext/http3"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://quic.example.com"),
        relayhttp3.WithHTTP3(),
    )
    if err != nil {
        log.Fatalf("relay.New: %v", err)
    }
    defer client.Close()

    ctx := context.Background()
    resp, err := client.Get(ctx, "/api/data")
    if err != nil {
        log.Fatalf("GET /api/data: %v", err)
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)
    log.Printf("protocol: %s, status: %d, body: %s",
        resp.Proto, resp.StatusCode, body)
}
```

## API Reference

### `relayhttp3.WithHTTP3()`

```go
func WithHTTP3() relay.Option
```

Replaces the relay transport with an HTTP/3-capable transport using sensible defaults. The transport:

- Attempts HTTP/3 over QUIC first.
- Falls back to HTTP/2 or HTTP/1.1 via TCP if the server does not advertise QUIC support via `Alt-Svc`.
- Reuses QUIC connections across requests to the same host.

### `relayhttp3.WithHTTP3Config(cfg)`

```go
func WithHTTP3Config(cfg HTTP3Config) relay.Option
```

Configures the HTTP/3 transport with explicit settings.

### `HTTP3Config` Struct

```go
type HTTP3Config struct {
    // MaxIdleConns is the maximum number of idle QUIC connections to keep
    // open across all hosts. 0 uses the default (100).
    MaxIdleConns int

    // MaxIdleConnsPerHost is the maximum number of idle QUIC connections
    // to keep per target host. 0 uses the default (10).
    MaxIdleConnsPerHost int

    // MaxConnsPerHost limits the total number of simultaneous QUIC
    // connections to a single host. 0 means no limit.
    MaxConnsPerHost int

    // DisableCompression disables transparent gzip decompression. Brotli
    // and zstd decompression are handled by the ext/brotli extension.
    DisableCompression bool

    // EnableDatagrams enables QUIC datagram support (RFC 9221).
    // Most HTTP/3 APIs do not use datagrams; enable only if required.
    EnableDatagrams bool

    // TLSClientConfig is the TLS configuration used for QUIC connections.
    // If nil, the system root CAs and default settings are used.
    TLSClientConfig *tls.Config

    // QUICConfig provides low-level QUIC protocol configuration.
    // If nil, quic-go defaults are used.
    QUICConfig *quic.Config

    // Versions specifies the acceptable QUIC versions in preference order.
    // If nil, the quic-go defaults are used (QUIC v1, draft-29).
    Versions []quic.Version

    // DialTimeout is the maximum time to wait when establishing a new
    // QUIC connection. 0 uses the relay client's overall timeout.
    DialTimeout time.Duration
}
```

**Field Reference:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `MaxIdleConns` | `int` | `100` | Pool size for idle QUIC connections. |
| `MaxIdleConnsPerHost` | `int` | `10` | Per-host idle connection limit. |
| `MaxConnsPerHost` | `int` | `0` (unlimited) | Hard cap on simultaneous connections per host. |
| `DisableCompression` | `bool` | `false` | Disable automatic gzip decompression. |
| `EnableDatagrams` | `bool` | `false` | Enable RFC 9221 QUIC datagrams. |
| `TLSClientConfig` | `*tls.Config` | system defaults | TLS settings for QUIC handshake. |
| `QUICConfig` | `*quic.Config` | quic-go defaults | Low-level QUIC protocol knobs. |
| `Versions` | `[]quic.Version` | quic-go defaults | QUIC version negotiation list. |
| `DialTimeout` | `time.Duration` | client timeout | Maximum QUIC handshake duration. |

## Alt-Svc Header Negotiation

HTTP/3 is discovered via the `Alt-Svc` response header sent by the server. A typical value looks like:

```
Alt-Svc: h3=":443"; ma=2592000
```

This tells the client that HTTP/3 is available on port 443 with a maximum age of 30 days. The relay HTTP/3 transport:

1. Sends the first request over HTTP/2 or HTTP/1.1 (TCP).
2. Reads the `Alt-Svc` header from the response.
3. Caches the QUIC availability for the advertised `ma` (max-age) duration.
4. Uses HTTP/3 for all subsequent requests to the same host.

To skip the initial TCP request and assume HTTP/3 is available immediately (useful when you know the server supports it), use `HTTP3Config` with `ForceHTTP3: true`:

```go
relayhttp3.WithHTTP3Config(relayhttp3.HTTP3Config{
    ForceHTTP3: true,
})
```

> **warning**
> Setting `ForceHTTP3: true` disables fallback to TCP-based protocols. If the server does not support HTTP/3 or QUIC is blocked by a firewall, requests will fail immediately.

## TLS Requirements

QUIC requires TLS 1.3. The relay HTTP/3 extension enforces this - any `TLSClientConfig` you provide must not restrict `MinVersion` to below `tls.VersionTLS13`:

```go
import (
    "crypto/tls"

    relayhttp3 "github.com/jhonsferg/relay/ext/http3"
)

cfg := relayhttp3.HTTP3Config{
    TLSClientConfig: &tls.Config{
        MinVersion: tls.VersionTLS13, // required - do not lower this
        ServerName: "quic.example.com",
    },
}
```

### Custom Certificate Authority

For private QUIC services with self-signed certificates:

```go
import (
    "crypto/tls"
    "crypto/x509"
    "os"
)

certPEM, err := os.ReadFile("ca.pem")
if err != nil {
    log.Fatalf("read CA: %v", err)
}

pool := x509.NewCertPool()
if !pool.AppendCertsFromPEM(certPEM) {
    log.Fatal("failed to parse CA certificate")
}

cfg := relayhttp3.HTTP3Config{
    TLSClientConfig: &tls.Config{
        MinVersion: tls.VersionTLS13,
        RootCAs:    pool,
    },
}

client, err := relay.New(
    relay.WithBaseURL("https://internal-quic.corp"),
    relayhttp3.WithHTTP3Config(cfg),
)
```

## Complete Example: HTTP/3 Client

This example creates a relay client that prefers HTTP/3 and falls back to TCP-based protocols, with explicit QUIC configuration for a production workload:

```go
package main

import (
    "context"
    "crypto/tls"
    "fmt"
    "io"
    "log"
    "time"

    "github.com/jhonsferg/relay"
    relayhttp3 "github.com/jhonsferg/relay/ext/http3"
    "github.com/quic-go/quic-go"
)

func main() {
    quicCfg := &quic.Config{
        // Allow the server to push up to 100 concurrent streams.
        MaxIncomingStreams: 100,

        // Keep QUIC connections alive for 90 seconds of inactivity.
        MaxIdleTimeout: 90 * time.Second,

        // Enable connection migration (useful for mobile clients or NAT rebinding).
        DisablePathMTUDiscovery: false,
    }

    h3cfg := relayhttp3.HTTP3Config{
        MaxIdleConns:     50,
        MaxConnsPerHost:  5,
        EnableDatagrams:  false,
        DialTimeout:      3 * time.Second,
        QUICConfig:       quicCfg,
        TLSClientConfig: &tls.Config{
            MinVersion: tls.VersionTLS13,
        },
    }

    client, err := relay.New(
        relay.WithBaseURL("https://cloudflare-quic.com"),
        relay.WithTimeout(15),
        relayhttp3.WithHTTP3Config(h3cfg),
    )
    if err != nil {
        log.Fatalf("relay.New: %v", err)
    }
    defer client.Close()

    ctx := context.Background()

    // The first request may use HTTP/2 until Alt-Svc is discovered.
    for i := 0; i < 5; i++ {
        resp, err := client.Get(ctx, "/")
        if err != nil {
            log.Printf("request %d failed: %v", i, err)
            continue
        }

        body, _ := io.ReadAll(resp.Body)
        resp.Body.Close()

        fmt.Printf("request %d: protocol=%s status=%d body_size=%d\n",
            i, resp.Proto, resp.StatusCode, len(body))
    }
}
```

Expected output after the `Alt-Svc` cache warms up:

```
request 0: protocol=HTTP/2.0 status=200 body_size=4096
request 1: protocol=HTTP/3.0 status=200 body_size=4096
request 2: protocol=HTTP/3.0 status=200 body_size=4096
request 3: protocol=HTTP/3.0 status=200 body_size=4096
request 4: protocol=HTTP/3.0 status=200 body_size=4096
```

## Combining HTTP/3 with Other Extensions

The HTTP/3 extension replaces the underlying transport. Other extensions that wrap the transport (tracing, metrics, logging) must be applied **after** `WithHTTP3Config` so they wrap the HTTP/3 transport:

```go
client, err := relay.New(
    relay.WithBaseURL("https://quic.example.com"),
    // 1. Set the HTTP/3 transport first (innermost layer).
    relayhttp3.WithHTTP3(),
    // 2. Wrap it with tracing and metrics.
    relaytracing.WithTracing(otel.GetTracerProvider(), otel.GetTextMapPropagator()),
    relaymetrics.WithOTelMetrics(otel.GetMeterProvider()),
)
```

> **tip**
> The relay option chain is applied in order. The last-applied transport wrapper becomes the outermost layer (runs first). Place HTTP/3 as the first option so all observability layers wrap the QUIC transport correctly.

## Troubleshooting

### QUIC Connections are Blocked

Many enterprise firewalls and some cloud providers block UDP traffic on port 443. If HTTP/3 requests consistently fail with connection errors but HTTP/2 requests succeed, check whether UDP is allowed to your target host:

```bash
# Test if UDP port 443 is reachable (requires nmap or nc with UDP support).
nc -u -z -w 3 quic.example.com 443
```

If UDP is blocked, use `relayhttp3.WithHTTP3()` without `ForceHTTP3: true` to allow automatic TCP fallback.

### Version Negotiation Failures

If the QUIC handshake fails with a version negotiation error, the server may support only newer or older QUIC draft versions. Specify compatible versions explicitly:

```go
relayhttp3.WithHTTP3Config(relayhttp3.HTTP3Config{
    Versions: []quic.Version{quic.Version1}, // RFC 9000 QUIC v1 only
})
```

## See Also

- [Extensions Overview](index.md)
- [WebSocket Extension](websocket.md)
