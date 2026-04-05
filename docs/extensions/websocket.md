# WebSocket Extension (Legacy)

> **warning**
> The `ext/websocket` package is a legacy extension retained for backward compatibility. The relay core package now provides first-class WebSocket support via `client.ExecuteWebSocket(...)`. All new code should use the core API. Migrate existing code following the guide in this document.

## Overview

WebSocket support in relay has two generations:

| Generation | Package | Status |
|------------|---------|--------|
| Legacy | `github.com/jhonsferg/relay/ext/websocket` | Retained for compatibility, no new features |
| Current | `github.com/jhonsferg/relay` (core) | Actively developed, recommended |

The core `ExecuteWebSocket` API was introduced because the legacy extension had several limitations:
- It bypassed relay's middleware and transport chain, so tracing, metrics, and retry hooks did not apply to WebSocket connections.
- It exposed the raw `gorilla/websocket` Upgrader, tying users to a specific WebSocket library.
- Connection lifecycle (ping/pong, reconnect) was the caller's responsibility with no helpers.

The core API addresses all of these by integrating fully with relay's option system and providing structured message handlers.

## Legacy API (ext/websocket)

If you have existing code using the legacy extension, this section documents its API for reference.

### Installation (Legacy)

```bash
go get github.com/jhonsferg/relay/ext/websocket
```

### Legacy Usage Example

```go
package main

import (
    "context"
    "log"

    "github.com/jhonsferg/relay"
    relayws "github.com/jhonsferg/relay/ext/websocket"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("wss://echo.example.com"),
        relayws.WithWebSocket(), // legacy option
    )
    if err != nil {
        log.Fatalf("relay.New: %v", err)
    }
    defer client.Close()

    ctx := context.Background()

    // Legacy API: returns a raw *websocket.Conn from gorilla/websocket.
    conn, _, err := client.DialWebSocket(ctx, "/ws")
    if err != nil {
        log.Fatalf("dial: %v", err)
    }
    defer conn.Close()

    if err := conn.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
        log.Fatalf("write: %v", err)
    }

    msgType, p, err := conn.ReadMessage()
    if err != nil {
        log.Fatalf("read: %v", err)
    }
    log.Printf("received type=%d: %s", msgType, p)
}
```

### Why Legacy Code Falls Short

The legacy `DialWebSocket` call returns a `*gorilla/websocket.Conn` directly. This has several consequences:

1. **No tracing.** Spans are not created for WebSocket connections or messages.
2. **No metrics.** `relay_request_total` and `relay_request_duration_ms` are not updated.
3. **No retry.** If the WebSocket server is temporarily unavailable, the dial fails immediately.
4. **Gorilla lock-in.** Switching the underlying library requires changing call sites.
5. **No automatic ping/pong.** Connection keep-alive is manual.

## Current Core API (ExecuteWebSocket)

The core API integrates WebSocket connections into relay's full lifecycle. Use `client.ExecuteWebSocket(ctx, path, handler)` where `handler` receives a `relay.WebSocketSession` interface.

### Core WebSocket Types

```go
// WebSocketSession is passed to your handler function.
// It is safe to use from multiple goroutines.
type WebSocketSession interface {
    // Send sends a text message to the remote peer.
    Send(ctx context.Context, msg string) error

    // SendBinary sends a binary message to the remote peer.
    SendBinary(ctx context.Context, data []byte) error

    // Receive blocks until a message arrives or the context is done.
    // Returns (message, messageType, error).
    Receive(ctx context.Context) (string, MessageType, error)

    // Close sends a WebSocket close frame with the given code and reason,
    // then shuts down the connection.
    Close(code int, reason string) error

    // RemoteAddr returns the address of the remote peer.
    RemoteAddr() net.Addr
}

// MessageType identifies the WebSocket frame type.
type MessageType int

const (
    TextMessage   MessageType = 1
    BinaryMessage MessageType = 2
    CloseMessage  MessageType = 8
    PingMessage   MessageType = 9
    PongMessage   MessageType = 10
)
```

### Full Example Using the Core API

The following example connects to a WebSocket echo server, sends five messages, receives their echoes, and closes cleanly:

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
        relay.WithBaseURL("wss://echo.websocket.org"),
        relay.WithTimeout(30),
        // Tracing and metrics work normally with ExecuteWebSocket.
        // relaytracing.WithTracing(...),
        // relaymetrics.WithOTelMetrics(...),
    )
    if err != nil {
        log.Fatalf("relay.New: %v", err)
    }
    defer client.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
    defer cancel()

    err = client.ExecuteWebSocket(ctx, "/", func(sess relay.WebSocketSession) error {
        // Send five messages and read the echo for each.
        for i := 1; i <= 5; i++ {
            msg := fmt.Sprintf("message %d", i)

            if err := sess.Send(ctx, msg); err != nil {
                return fmt.Errorf("send: %w", err)
            }
            log.Printf("sent: %s", msg)

            reply, _, err := sess.Receive(ctx)
            if err != nil {
                return fmt.Errorf("receive: %w", err)
            }
            log.Printf("received: %s", reply)

            time.Sleep(500 * time.Millisecond)
        }

        // Close the connection gracefully.
        return sess.Close(1000, "done")
    })
    if err != nil {
        log.Fatalf("ExecuteWebSocket: %v", err)
    }
}
```

### Real-time Streaming Example

This example subscribes to a streaming market data feed and processes messages in a read loop:

```go
package main

import (
    "context"
    "encoding/json"
    "log"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/jhonsferg/relay"
)

type TickerEvent struct {
    Type  string  `json:"type"`
    Price float64 `json:"price"`
    Seq   int64   `json:"sequence"`
}

func main() {
    client, err := relay.New(
        relay.WithBaseURL("wss://stream.exchange.example.com"),
        relay.WithHeader("Authorization", "Bearer "+os.Getenv("API_TOKEN")),
    )
    if err != nil {
        log.Fatalf("relay.New: %v", err)
    }
    defer client.Close()

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Cancel on OS signal.
    sig := make(chan os.Signal, 1)
    signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
    go func() {
        <-sig
        cancel()
    }()

    err = client.ExecuteWebSocket(ctx, "/v2/feed", func(sess relay.WebSocketSession) error {
        // Subscribe to the BTC-USD ticker.
        sub := map[string]any{
            "type":       "subscribe",
            "product_id": "BTC-USD",
        }
        payload, _ := json.Marshal(sub)
        if err := sess.SendBinary(ctx, payload); err != nil {
            return fmt.Errorf("subscribe: %w", err)
        }

        // Read messages until the context is cancelled.
        for {
            msg, _, err := sess.Receive(ctx)
            if err != nil {
                if ctx.Err() != nil {
                    return nil // normal shutdown
                }
                return fmt.Errorf("receive: %w", err)
            }

            var event TickerEvent
            if err := json.Unmarshal([]byte(msg), &event); err != nil {
                log.Printf("parse error: %v (raw: %s)", err, msg)
                continue
            }

            log.Printf("BTC-USD price: %.2f (seq: %d)", event.Price, event.Seq)
        }
    })
    if err != nil {
        log.Fatalf("ExecuteWebSocket: %v", err)
    }
}
```

## Migration Guide: ext/websocket to Core API

The migration is straightforward. The table below maps legacy calls to their core equivalents.

| Legacy | Core |
|--------|------|
| `relayws.WithWebSocket()` option | No option needed - `ExecuteWebSocket` is always available |
| `client.DialWebSocket(ctx, path)` | `client.ExecuteWebSocket(ctx, path, handler)` |
| `conn.WriteMessage(TextMessage, []byte(s))` | `sess.Send(ctx, s)` |
| `conn.WriteMessage(BinaryMessage, data)` | `sess.SendBinary(ctx, data)` |
| `conn.ReadMessage()` | `sess.Receive(ctx)` |
| `conn.Close()` | `sess.Close(1000, "normal closure")` |
| Manual ping/pong goroutine | Automatic (configured via `relay.WithWebSocketPingInterval`) |

### Before (Legacy)

```go
import (
    "github.com/jhonsferg/relay"
    relayws "github.com/jhonsferg/relay/ext/websocket"
    "github.com/gorilla/websocket"
)

client, _ := relay.New(
    relay.WithBaseURL("wss://api.example.com"),
    relayws.WithWebSocket(),
)

conn, _, err := client.DialWebSocket(ctx, "/stream")
if err != nil {
    log.Fatal(err)
}
defer conn.Close()

// Manual keep-alive goroutine.
go func() {
    ticker := time.NewTicker(25 * time.Second)
    defer ticker.Stop()
    for range ticker.C {
        if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
            return
        }
    }
}()

conn.WriteMessage(websocket.TextMessage, []byte("hello"))
_, p, err := conn.ReadMessage()
```

### After (Core)

```go
import "github.com/jhonsferg/relay"

client, _ := relay.New(
    relay.WithBaseURL("wss://api.example.com"),
    // No WebSocket option required.
    relay.WithWebSocketPingInterval(25*time.Second), // automatic ping/pong
)

err = client.ExecuteWebSocket(ctx, "/stream", func(sess relay.WebSocketSession) error {
    // No manual ping goroutine needed.
    if err := sess.Send(ctx, "hello"); err != nil {
        return err
    }
    p, _, err := sess.Receive(ctx)
    if err != nil {
        return err
    }
    log.Printf("received: %s", p)
    return sess.Close(1000, "done")
})
```

## Why Migrate?

- **Observability.** Tracing and metrics extensions automatically instrument WebSocket connections when using the core API.
- **Automatic reconnect.** Configure `relay.WithWebSocketReconnect(maxAttempts, backoff)` to retry dropped connections without custom retry loops.
- **Automatic ping/pong.** Set `relay.WithWebSocketPingInterval(d)` to keep connections alive without a background goroutine.
- **Cleaner error handling.** Return an error from the handler function to signal failure; the client propagates it from `ExecuteWebSocket`.
- **Library independence.** The `WebSocketSession` interface is not tied to `gorilla/websocket`. relay can switch the underlying implementation without breaking your code.
- **Context propagation.** Passing `ctx` to every `Send` and `Receive` call means cancellation and deadlines work correctly with no extra plumbing.

## See Also

- [HTTP/3 Extension](http3.md) - QUIC/HTTP3 transport
- [Extensions Overview](index.md)
