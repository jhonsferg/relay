# Enhanced SSE

relay provides three ways to consume a [Server-Sent Events](https://html.spec.whatwg.org/multipage/server-sent-events.html) stream, each suited to a different use case:

| Method | Use when |
|---|---|
| `ExecuteSSE` | Simple streaming with a callback, no reconnect |
| `ExecuteSSEWithReconnect` | Long-lived streams that must survive disconnects |
| `ExecuteSSEStream` | Channel-based consumption, fan-out, or `select` loops |

---

## SSEEvent

```go
type SSEEvent struct {
    ID    string
    Event string // defaults to "message" when absent
    Data  string
    Retry int    // reconnect hint in milliseconds; 0 when absent
}
```

Each `SSEEvent` corresponds to one fully-parsed event block from the stream. Multi-line `data` fields are joined with a newline.

---

## SSEClientConfig

`SSEClientConfig` controls the reconnect behaviour of `ExecuteSSEWithReconnect`.

```go
type SSEClientConfig struct {
    // MaxReconnects is the maximum number of reconnect attempts.
    // 0 means unlimited.
    MaxReconnects int

    // ReconnectDelay is the base delay between reconnects.
    // If the server sends a "retry" field, that takes precedence.
    // Defaults to 3s when zero.
    ReconnectDelay time.Duration

    // EventTypes filters which event types are delivered to the handler.
    // An empty slice delivers all events.
    EventTypes []string
}
```

---

## ExecuteSSEWithReconnect

```go
func (c *Client) ExecuteSSEWithReconnect(
    req *Request,
    cfg SSEClientConfig,
    handler SSEHandler,
) error
```

`ExecuteSSEWithReconnect` opens the SSE stream and automatically reconnects on disconnect. On each reconnect it sends the `Last-Event-ID` header with the last received event ID, allowing the server to resume from where it left off.

**Parameters:**

- `req` -- the request to send. Typically a `GET` with `Accept: text/event-stream`.
- `cfg` -- reconnect settings (delay, max attempts, event-type filter).
- `handler` -- callback invoked for each event; return `false` to stop the stream.

**Reconnect logic:**

1. Send the request (with `Last-Event-ID` on subsequent attempts).
2. Read events until the stream closes or the handler returns `false`.
3. If the stream closed normally, reconnect after `cfg.ReconnectDelay` (or the server-supplied `retry` value).
4. Stop after `cfg.MaxReconnects` attempts (or never, if `MaxReconnects` is 0).

### Basic usage

```go
package main

import (
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
    )
    if err != nil {
        log.Fatal(err)
    }

    cfg := relay.SSEClientConfig{
        MaxReconnects:  5,
        ReconnectDelay: 2 * time.Second,
    }

    err = client.ExecuteSSEWithReconnect(
        client.NewRequest("GET", "/events").
            WithHeader("Accept", "text/event-stream"),
        cfg,
        func(ev relay.SSEEvent) bool {
            fmt.Printf("[%s] %s\n", ev.Event, ev.Data)
            return true // continue
        },
    )
    if err != nil {
        log.Fatal(err)
    }
}
```

### Event-type filtering

Pass a list of event types to `EventTypes`. Only events whose `event` field matches one of the listed types are delivered to the handler; all others are silently discarded.

```go
package main

import (
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://notifications.example.com"),
    )
    if err != nil {
        log.Fatal(err)
    }

    cfg := relay.SSEClientConfig{
        MaxReconnects:  10,
        ReconnectDelay: 3 * time.Second,
        // Only receive "order-update" and "payment-confirmed" events
        EventTypes: []string{"order-update", "payment-confirmed"},
    }

    err = client.ExecuteSSEWithReconnect(
        client.NewRequest("GET", "/stream").
            WithHeader("Accept", "text/event-stream"),
        cfg,
        func(ev relay.SSEEvent) bool {
            switch ev.Event {
            case "order-update":
                fmt.Println("order updated:", ev.Data)
            case "payment-confirmed":
                fmt.Println("payment confirmed:", ev.Data)
            }
            return true
        },
    )
    if err != nil {
        log.Fatal(err)
    }
}
```

### Stopping early

Return `false` from the handler to close the connection immediately without reconnecting.

```go
package main

import (
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
    )
    if err != nil {
        log.Fatal(err)
    }

    received := 0
    err = client.ExecuteSSEWithReconnect(
        client.NewRequest("GET", "/events").
            WithHeader("Accept", "text/event-stream"),
        relay.SSEClientConfig{},
        func(ev relay.SSEEvent) bool {
            fmt.Println(ev.Data)
            received++
            return received < 5 // stop after 5 events
        },
    )
    if err != nil {
        log.Fatal(err)
    }
}
```

---

## ExecuteSSEStream

```go
func (c *Client) ExecuteSSEStream(
    ctx context.Context,
    req *Request,
) (<-chan SSEEvent, <-chan error)
```

`ExecuteSSEStream` returns two channels: one for events and one for errors. The stream runs in a background goroutine. Cancel `ctx` to stop it.

Both channels are closed when the stream ends (normally or on error). The caller **must** read from both channels until they are closed, or use a `select` loop that also listens on the context.

### Basic channel consumption

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
    )
    if err != nil {
        log.Fatal(err)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    events, errs := client.ExecuteSSEStream(
        ctx,
        client.NewRequest("GET", "/events").
            WithHeader("Accept", "text/event-stream"),
    )

    for {
        select {
        case ev, ok := <-events:
            if !ok {
                return // stream closed
            }
            fmt.Printf("[%s] %s\n", ev.Event, ev.Data)

        case err, ok := <-errs:
            if !ok {
                return
            }
            log.Printf("stream error: %v", err)
            return
        }
    }
}
```

### Fan-out to multiple consumers

Because `ExecuteSSEStream` returns plain Go channels, you can fan out to multiple consumers using a dispatcher goroutine.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func fanOut(in <-chan relay.SSEEvent, n int) []<-chan relay.SSEEvent {
    outs := make([]chan relay.SSEEvent, n)
    for i := range outs {
        outs[i] = make(chan relay.SSEEvent, 16)
    }
    go func() {
        for ev := range in {
            for _, out := range outs {
                out <- ev
            }
        }
        for _, out := range outs {
            close(out)
        }
    }()
    result := make([]<-chan relay.SSEEvent, n)
    for i, ch := range outs {
        result[i] = ch
    }
    return result
}

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
    )
    if err != nil {
        log.Fatal(err)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    events, errs := client.ExecuteSSEStream(
        ctx,
        client.NewRequest("GET", "/events").
            WithHeader("Accept", "text/event-stream"),
    )

    consumers := fanOut(events, 3)

    go func() {
        for err := range errs {
            log.Printf("stream error: %v", err)
        }
    }()

    // Each consumer processes events independently
    for i, ch := range consumers {
        i := i
        go func(c <-chan relay.SSEEvent) {
            for ev := range c {
                fmt.Printf("consumer %d: %s\n", i, ev.Data)
            }
        }(ch)
    }

    // Block until context cancelled
    <-ctx.Done()
}
```

> **note**
> `ExecuteSSEStream` does not reconnect automatically. Combine it with your own retry loop, or use `ExecuteSSEWithReconnect` when reconnection is needed.

---

## Choosing the Right Method

| Requirement | Method |
|---|---|
| Simple one-shot stream with callback | `ExecuteSSE` |
| Durable stream with auto-reconnect | `ExecuteSSEWithReconnect` |
| Channel-based or `select`-driven consumption | `ExecuteSSEStream` |
| Fan-out to multiple consumers | `ExecuteSSEStream` + dispatcher goroutine |
| Filter by event type | `ExecuteSSEWithReconnect` with `SSEClientConfig.EventTypes` |

---

## Last-Event-ID Resumption

`ExecuteSSEWithReconnect` tracks the `id` field of the last received event. On every reconnect it sends this value in the `Last-Event-ID` request header. A well-behaved SSE server uses this header to replay missed events, providing at-least-once delivery across reconnects.

```
Client                            Server
  │                                 │
  │── GET /events ─────────────────▶│
  │◀─ 200 OK (SSE stream) ──────────│
  │◀─ id: 42, data: "hello" ────────│
  │                  (disconnect)   │
  │── GET /events ─────────────────▶│
  │   Last-Event-ID: 42             │
  │◀─ 200 OK (SSE stream) ──────────│
  │◀─ id: 43, data: "world" ────────│  ← resumes from event 43
```
