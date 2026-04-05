# WebSocket

relay can upgrade an HTTP connection to a WebSocket connection using the same
client instance that handles your regular HTTP traffic. This means TLS
configuration, default headers, and request signing all apply automatically
to the WebSocket handshake.

---

## Overview

```go
// ExecuteWebSocket upgrades req to a WebSocket connection.
// req may target ws://, wss://, http://, or https:// - relay rewrites
// http/https to ws/wss transparently.
// The caller must call WSConn.Close() when done.
func (c *Client) ExecuteWebSocket(ctx context.Context, req *relay.Request) (*relay.WSConn, error)

// WSConn is an active WebSocket connection.
type WSConn struct { /* unexported */ }

func (c *WSConn) Read(ctx context.Context) ([]byte, error)
func (c *WSConn) Write(ctx context.Context, data []byte) error
func (c *WSConn) Close() error
```

---

## Connecting to a WebSocket server

```go
package main

import (
    "context"
    "fmt"
    "time"

    relay "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(
        relay.WithBaseURL("wss://echo.websocket.org"),
        relay.WithWebSocketDialTimeout(5 * time.Second),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()

    conn, err := client.ExecuteWebSocket(ctx, client.Get("/"))
    if err != nil {
        fmt.Println("dial error:", err)
        return
    }
    defer conn.Close()

    fmt.Println("connected to WebSocket server")
}
```

---

## Reading and writing messages

`WSConn.Write` sends a binary message; `WSConn.Read` blocks until the next
message arrives, the server closes the connection, or the context is cancelled.

```go
package main

import (
    "context"
    "fmt"
    "time"

    relay "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("wss://echo.websocket.org"))
    defer client.Shutdown(context.Background()) //nolint:errcheck

    ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
    defer cancel()

    conn, err := client.ExecuteWebSocket(ctx, client.Get("/"))
    if err != nil {
        fmt.Println("dial:", err)
        return
    }
    defer conn.Close()

    // Send a message.
    if err := conn.Write(ctx, []byte("hello, server!")); err != nil {
        fmt.Println("write:", err)
        return
    }

    // Read the echo.
    msg, err := conn.Read(ctx)
    if err != nil {
        fmt.Println("read:", err)
        return
    }
    fmt.Printf("received: %s\n", msg)
}
```

---

## WithWebSocketDialTimeout

`WithWebSocketDialTimeout` sets the maximum time allowed for the TCP dial and
HTTP upgrade handshake. It is separate from the relay client's general
`WithTimeout` so you can tune them independently:

```go
package main

import (
    "context"
    "fmt"
    "time"

    relay "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(
        relay.WithBaseURL("wss://realtime.example.com"),

        // Allow up to 30 s for regular HTTP requests.
        relay.WithTimeout(30*time.Second),

        // But only 3 s to establish the WebSocket handshake.
        relay.WithWebSocketDialTimeout(3*time.Second),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    ctx := context.Background()
    conn, err := client.ExecuteWebSocket(ctx, client.Get("/events"))
    if err != nil {
        fmt.Println("handshake failed:", err)
        return
    }
    defer conn.Close()
    fmt.Println("connected")
}
```

> **Note**
> If `WithWebSocketDialTimeout` is not set, relay falls back to the value
> configured by `WithTimeout`. If neither is set, Go's default dial timeout
> applies.

---

## Handling disconnections and reconnection pattern

WebSocket connections can drop due to network issues, server restarts, or idle
timeouts. Implement a reconnect loop that backs off exponentially on repeated
failures:

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "io"
    "log/slog"
    "time"

    relay "github.com/jhonsferg/relay"
)

type EventHandler func(data []byte)

// streamWithReconnect connects to wsURL and calls handler for each message.
// It reconnects automatically after disconnections with exponential back-off.
func streamWithReconnect(ctx context.Context, client *relay.Client, path string, handler EventHandler) {
    const (
        baseDelay = 500 * time.Millisecond
        maxDelay  = 30 * time.Second
    )
    delay := baseDelay

    for {
        if ctx.Err() != nil {
            return
        }

        slog.InfoContext(ctx, "connecting to WebSocket", "path", path)
        conn, err := client.ExecuteWebSocket(ctx, client.Get(path))
        if err != nil {
            slog.WarnContext(ctx, "dial failed", "err", err, "retry_in", delay)
            select {
            case <-ctx.Done():
                return
            case <-time.After(delay):
            }
            delay = min(delay*2, maxDelay)
            continue
        }

        // Reset back-off after a successful connection.
        delay = baseDelay
        slog.InfoContext(ctx, "connected")

        // Read loop.
        for {
            msg, readErr := conn.Read(ctx)
            if readErr != nil {
                conn.Close()
                if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
                    return
                }
                if errors.Is(readErr, io.EOF) {
                    slog.InfoContext(ctx, "server closed connection, reconnecting")
                } else {
                    slog.WarnContext(ctx, "read error, reconnecting", "err", readErr)
                }
                break // break inner loop, reconnect
            }
            handler(msg)
        }

        select {
        case <-ctx.Done():
            return
        case <-time.After(delay):
        }
        delay = min(delay*2, maxDelay)
    }
}

func min(a, b time.Duration) time.Duration {
    if a < b {
        return a
    }
    return b
}

func main() {
    client := relay.New(
        relay.WithBaseURL("wss://realtime.example.com"),
        relay.WithWebSocketDialTimeout(5*time.Second),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()

    streamWithReconnect(ctx, client, "/stream", func(data []byte) {
        fmt.Printf("event: %s\n", data)
    })
}
```

---

## Ping/pong keepalive

Many WebSocket servers disconnect idle clients after a period of inactivity.
Send periodic pings from a background goroutine to keep the connection alive:

```go
package main

import (
    "context"
    "fmt"
    "log/slog"
    "time"

    relay "github.com/jhonsferg/relay"
)

// keepAlive sends a ping message every interval until ctx is cancelled.
func keepAlive(ctx context.Context, conn *relay.WSConn, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            if err := conn.Write(ctx, []byte("ping")); err != nil {
                slog.WarnContext(ctx, "keepalive write failed", "err", err)
                return
            }
        }
    }
}

func main() {
    client := relay.New(
        relay.WithBaseURL("wss://realtime.example.com"),
        relay.WithWebSocketDialTimeout(5*time.Second),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    conn, err := client.ExecuteWebSocket(ctx, client.Get("/ws"))
    if err != nil {
        fmt.Println("dial:", err)
        return
    }
    defer conn.Close()

    // Start keepalive in the background.
    go keepAlive(ctx, conn, 20*time.Second)

    // Main read loop.
    for {
        msg, err := conn.Read(ctx)
        if err != nil {
            fmt.Println("read:", err)
            return
        }
        if string(msg) == "pong" {
            continue // ignore pong replies
        }
        fmt.Printf("message: %s\n", msg)
    }
}
```

> **Tip**
> The keepalive interval should be shorter than the server's idle timeout.
> Check the server documentation; 20-30 seconds is a safe starting point for
> most cloud services.

---

## Full example: chat client

The following example implements a minimal terminal chat client that reads
messages from stdin and prints incoming messages from the server:

```go
package main

import (
    "bufio"
    "context"
    "fmt"
    "io"
    "log/slog"
    "os"
    "os/signal"
    "strings"
    "syscall"
    "time"

    relay "github.com/jhonsferg/relay"
)

func main() {
    if len(os.Args) < 2 {
        fmt.Fprintln(os.Stderr, "usage: chat <wss://host/path>")
        os.Exit(1)
    }
    serverURL := os.Args[1]

    client := relay.New(
        relay.WithWebSocketDialTimeout(5*time.Second),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Cancel context on SIGINT / SIGTERM so we shut down cleanly.
    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
    go func() {
        <-sig
        cancel()
    }()

    conn, err := client.ExecuteWebSocket(ctx, client.Get(serverURL))
    if err != nil {
        fmt.Fprintln(os.Stderr, "connect:", err)
        os.Exit(1)
    }
    defer conn.Close()

    fmt.Println("connected - type messages and press Enter. Ctrl+C to quit.")

    // Goroutine: print incoming messages.
    go func() {
        for {
            msg, err := conn.Read(ctx)
            if err != nil {
                if ctx.Err() != nil {
                    return
                }
                slog.Warn("read error", "err", err)
                cancel()
                return
            }
            fmt.Printf("\r[server] %s\n> ", msg)
        }
    }()

    // Main goroutine: read lines from stdin and send them.
    scanner := bufio.NewScanner(os.Stdin)
    fmt.Print("> ")
    for scanner.Scan() {
        if ctx.Err() != nil {
            break
        }
        line := strings.TrimSpace(scanner.Text())
        if line == "" {
            fmt.Print("> ")
            continue
        }
        if err := conn.Write(ctx, []byte(line)); err != nil {
            if ctx.Err() != nil {
                break
            }
            fmt.Fprintln(os.Stderr, "write:", err)
            break
        }
        fmt.Print("> ")
    }
    if err := scanner.Err(); err != nil && err != io.EOF {
        fmt.Fprintln(os.Stderr, "stdin:", err)
    }

    fmt.Println("\ndisconnected")
}
```

---

## Authenticating WebSocket connections

relay applies default headers and the configured `Signer` to the upgrade
handshake, so authentication works identically to regular HTTP requests:

```go
package main

import (
    "context"
    "fmt"
    "time"

    relay "github.com/jhonsferg/relay"
)

func main() {
    // Option 1: static header on the client.
    client := relay.New(
        relay.WithBaseURL("wss://api.example.com"),
        relay.WithDefaultHeader("Authorization", "Bearer my-token"),
        relay.WithWebSocketDialTimeout(5*time.Second),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Option 2: per-request header (overrides the default).
    conn, err := client.ExecuteWebSocket(
        ctx,
        client.Get("/ws/events").Header("Authorization", "Bearer per-request-token"),
    )
    if err != nil {
        fmt.Println("dial:", err)
        return
    }
    defer conn.Close()

    msg, _ := conn.Read(ctx)
    fmt.Printf("first message: %s\n", msg)
}
```

> **Warning**
> Do not pass credentials in the WebSocket URL query string
> (`wss://host/ws?token=secret`). Query strings are logged by proxies and
> appear in server access logs. Use headers or a first-message authentication
> handshake instead.
