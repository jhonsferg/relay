# HTTP/2 Push

relay provides a `PushedResponseCache` and `WithHTTP2PushHandler` to intercept and store HTTP/2 server-push promises, letting you serve subsequent requests from the cache rather than waiting for a round trip.

---

## Current Limitation

> **⚠️ Forward-compatibility API only.**
>
> `golang.org/x/net` v0.50.0 (the version currently used by relay) removed the public `PushHandler` interface from `http2.Transport` and the client-side `SETTINGS_ENABLE_PUSH=0` frame is sent unconditionally. As a result, `WithHTTP2PushHandler` is a **no-op at runtime**: the handler is stored in the client config but is never called.
>
> The API is intentionally kept in the public surface so call-sites are forward-compatible: once upstream restores push interception support the stored handler will be wired in without any breaking change.

---

## Usage

```go
package main

import (
    "context"
    "fmt"
    "io"
    "log"
    "net/http"

    "github.com/jhonsferg/relay"
)

func main() {
    cache := relay.NewPushedResponseCache()

    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithHTTP2PushHandler(func(pushedURL string, pushedResp *http.Response) {
            defer pushedResp.Body.Close() //nolint:errcheck
            // Consume the body so the connection is not blocked.
            body, err := io.ReadAll(pushedResp.Body)
            if err != nil {
                return
            }
            // Store a reconstructed response in the cache.
            pushedResp.Body = io.NopCloser(
                func() io.Reader {
                    return newBytesReader(body)
                }(),
            )
            cache.Store(pushedURL, pushedResp)
            fmt.Println("cached pushed resource:", pushedURL)
        }),
    )
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()

    // First request; the server may push /styles.css.
    resp, err := client.Get(ctx, "/page", nil)
    if err != nil {
        log.Fatal(err)
    }
    resp.Body.Close() //nolint:errcheck

    // Check whether the pushed resource is already in cache.
    if pushed, ok := cache.Load("https://api.example.com/styles.css"); ok {
        fmt.Printf("served from push cache: %d\n", pushed.StatusCode)
        pushed.Body.Close() //nolint:errcheck
    }

    fmt.Println("cache size:", cache.Len())
}
```

---

## API Reference

### `PushPromiseHandler`

```go
type PushPromiseHandler func(pushedURL string, pushedResp *http.Response)
```

Callback invoked when a server-push promise is received. The handler is responsible for consuming **and closing** `pushedResp.Body`.

### `WithHTTP2PushHandler`

```go
func WithHTTP2PushHandler(handler PushPromiseHandler) Option
```

Registers the handler on the client. Currently a no-op at runtime (see limitation above).

### `NewPushedResponseCache`

```go
func NewPushedResponseCache() *PushedResponseCache
```

Returns an initialised, empty cache. Safe for concurrent use.

### `Store`

```go
func (c *PushedResponseCache) Store(url string, resp *http.Response)
```

Stores a pushed response keyed by URL. Any previous entry for the same URL is silently replaced.

### `Load`

```go
func (c *PushedResponseCache) Load(url string) (*http.Response, bool)
```

Retrieves **and removes** the pushed response for `url`. Returns `nil, false` when no entry exists. The remove-on-read behaviour prevents stale responses from being served twice.

### `Len`

```go
func (c *PushedResponseCache) Len() int
```

Returns the number of entries currently held.

---

## Notes

- `PushedResponseCache` uses a `sync.RWMutex` internally and is safe for concurrent use from multiple goroutines.
- `Load` deletes the entry after reading it. If you need to serve a pushed resource multiple times, re-store it after reading.
- The `PushPromiseHandler` must consume and close `pushedResp.Body`; failing to do so will leak the underlying HTTP/2 stream.
