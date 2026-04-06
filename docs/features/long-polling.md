# Long Polling

Long polling is a technique for receiving server-pushed updates over plain HTTP. The client sends a request and the server holds the connection open until new data is available (or a timeout elapses). When the server responds, the client processes the data and immediately issues the next request -- creating a near-real-time update loop without requiring WebSockets or SSE infrastructure.

relay implements long polling through `ExecuteLongPoll`, which handles conditional requests via ETags so that unchanged responses consume minimal bandwidth.

---

## LongPollResult

```go
type LongPollResult struct {
    Modified bool      // true when the server returned new data (not 304)
    Response *Response // the full response when Modified is true; nil on 304
    ETag     string    // ETag from the response, to be passed to the next poll
}
```

| Field | Description |
|---|---|
| `Modified` | `true` when the server returned `200` (or another non-304 status). `false` on `304 Not Modified`. |
| `Response` | The full response object. `nil` when `Modified` is `false`. |
| `ETag` | The `ETag` header value from the response. Pass this back as `prevETag` in the next call to avoid re-receiving unchanged data. |

---

## ExecuteLongPoll

```go
func (c *Client) ExecuteLongPoll(
    ctx    context.Context,
    req    *Request,
    prevETag string,
    timeout  time.Duration,
) (LongPollResult, error)
```

`ExecuteLongPoll` sends a single long-polling request. It:

1. Sets the request timeout to `timeout`.
2. Adds `If-None-Match: <prevETag>` when `prevETag` is non-empty.
3. Returns `LongPollResult{Modified: false}` on `304 Not Modified`.
4. Returns `LongPollResult{Modified: true, Response: resp}` for any other success status.

**Parameters:**

| Parameter | Description |
|---|---|
| `ctx` | Controls the overall deadline. Cancelling `ctx` aborts the in-flight request. |
| `req` | The request to send. Usually a `GET` to the polling endpoint. |
| `prevETag` | The ETag from the previous poll. Pass `""` on the first call. |
| `timeout` | How long the server should hold the connection. A value of 55s is common to stay within typical 60s proxy timeouts. |

---

## Basic Usage

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
        relay.WithBaseURL("https://api.example.com"),
    )
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()
    var etag string

    for {
        result, err := client.ExecuteLongPoll(
            ctx,
            client.NewRequest("GET", "/updates"),
            etag,
            55*time.Second,
        )
        if err != nil {
            log.Printf("poll error: %v", err)
            time.Sleep(5 * time.Second) // back off on error
            continue
        }

        etag = result.ETag // carry ETag into the next poll

        if result.Modified {
            body, _ := result.Response.BodyString()
            fmt.Println("new data:", body)
        }
        // If not modified, loop immediately for the next hold
    }
}
```

---

## ETag-Based Change Detection

Passing `prevETag` from the previous result to the next call enables efficient conditional requests. The server compares the client's ETag against its current version and responds with `304 Not Modified` (no body) when nothing has changed, saving bandwidth and parse time.

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

type Config struct {
    FeatureFlags map[string]bool `json:"feature_flags"`
}

func pollConfig(ctx context.Context, client *relay.Client) {
    var (
        etag   string
        config Config
    )

    for {
        result, err := client.ExecuteLongPoll(
            ctx,
            client.NewRequest("GET", "/config"),
            etag,
            30*time.Second,
        )
        if err != nil {
            if ctx.Err() != nil {
                return // context cancelled, clean shutdown
            }
            log.Printf("config poll error: %v", err)
            time.Sleep(10 * time.Second)
            continue
        }

        etag = result.ETag

        if !result.Modified {
            // Server confirmed config has not changed -- nothing to do
            continue
        }

        if err := json.NewDecoder(result.Response.Body).Decode(&config); err != nil {
            log.Printf("decode error: %v", err)
            continue
        }

        fmt.Printf("config updated: %+v\n", config.FeatureFlags)
    }
}

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://config.example.com"),
    )
    if err != nil {
        log.Fatal(err)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    pollConfig(ctx, client)
}
```

---

## Graceful Shutdown

Cancel the context to abort an in-progress long poll during application shutdown. `ExecuteLongPoll` returns `context.Canceled` immediately.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "os/signal"
    "syscall"
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

    ctx, cancel := context.WithCancel(context.Background())

    // Cancel on SIGINT / SIGTERM
    go func() {
        sig := make(chan os.Signal, 1)
        signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
        <-sig
        fmt.Println("shutting down...")
        cancel()
    }()

    var etag string
    for {
        result, err := client.ExecuteLongPoll(
            ctx,
            client.NewRequest("GET", "/jobs"),
            etag,
            55*time.Second,
        )
        if err != nil {
            if ctx.Err() != nil {
                log.Println("long poll stopped")
                return
            }
            log.Printf("poll error: %v", err)
            time.Sleep(5 * time.Second)
            continue
        }

        etag = result.ETag
        if result.Modified {
            body, _ := result.Response.BodyString()
            fmt.Println("job update:", body)
        }
    }
}
```

---

## Timeout Selection

Choose `timeout` to stay within infrastructure limits:

| Layer | Typical limit | Recommended `timeout` |
|---|---|---|
| CDN / load balancer | 60s | 55s |
| API gateway | 30s | 25s |
| No proxy | unlimited | 60--90s |

A server that receives a long-poll request should respond before the client's timeout, either with new data or with `304`. If the server sends nothing and the timeout elapses, `ExecuteLongPoll` returns a normal (non-error) result -- the loop simply issues the next request.

---

## Long Polling vs SSE vs WebSocket

| Criterion | Long Polling | SSE | WebSocket |
|---|---|---|---|
| Protocol | Plain HTTP | HTTP streaming | Upgraded connection |
| Server push | One event per request | Continuous stream | Bidirectional |
| Proxy / firewall support | Excellent | Good | Variable |
| Reconnect handling | Caller's loop | Built-in (`ExecuteSSEWithReconnect`) | Caller's loop |
| Best for | Infrequent updates, simple servers | Continuous feeds | Chat, games, RPC |

Long polling is a good default when the update frequency is low (under a few events per second), the infrastructure does not support SSE, or you need maximum compatibility with existing HTTP proxies.

---

## Summary

| Concern | Recommendation |
|---|---|
| First poll | Pass `prevETag: ""` |
| Subsequent polls | Pass `result.ETag` from the previous call |
| No change | `result.Modified == false`, loop immediately |
| New data | `result.Modified == true`, read `result.Response` |
| Abort in-flight poll | Cancel the `context.Context` |
| Timeout value | 5--10s below the shortest upstream proxy timeout |
