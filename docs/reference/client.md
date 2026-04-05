# Client API Reference

The `relay.Client` is the central type in the relay library. It holds all configuration, manages connection pools, applies middleware, and dispatches HTTP requests. Clients are safe for concurrent use by multiple goroutines and are intended to be long-lived - create one at application startup and reuse it throughout.

---

## relay.New

```go
func New(opts ...relay.Option) *relay.Client
```

`New` constructs and returns a new `*relay.Client`. All configuration is applied through functional options. Calling `New` with no arguments produces a client with sensible defaults: a 30-second timeout, standard Go HTTP transport, no retries, and no circuit breaker.

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `opts` | `...relay.Option` | Zero or more option functions that configure the client. |

### Return Values

Returns a fully initialized `*relay.Client` ready for use.

> **Note:** `relay.New` never returns an error. Invalid option combinations (such as conflicting TLS settings) will panic at construction time rather than returning a nil client.

### Example - Minimal client

```go
package main

import "github.com/jhonsferg/relay"

func main() {
    client := relay.New()
    _ = client
}
```

### Example - Production client with full configuration

```go
package main

import (
    "crypto/tls"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithBearerToken("my-secret-token"),
        relay.WithTimeout(15*time.Second),
        relay.WithRetry(&relay.RetryConfig{
            MaxAttempts: 4,
            WaitMin:     200 * time.Millisecond,
            WaitMax:     2 * time.Second,
        }),
        relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
            MaxFailures: 5,
            Timeout:     30 * time.Second,
        }),
        relay.WithTLSConfig(&tls.Config{
            MinVersion: tls.VersionTLS12,
        }),
    )

    log.Println("client ready:", client)
}
```

---

## client.Execute

```go
func (c *Client) Execute(ctx context.Context, req *relay.Request) (*relay.Response, error)
```

`Execute` dispatches the provided request and returns the server response or an error. It applies all middleware, retries, circuit breaker logic, rate limiting, and other configured behaviors before and after the actual HTTP round-trip.

`Execute` is the lowest-level dispatch method. The shorthand methods (`Get`, `Post`, etc.) all eventually call `Execute` internally after building a request.

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `ctx` | `context.Context` | Controls cancellation and deadline for the entire request lifecycle, including all retry attempts. |
| `req` | `*relay.Request` | The request to execute, created via one of the builder methods. |

### Return Values

| Value | Description |
|-------|-------------|
| `*relay.Response` | Non-nil on HTTP-level success (including 4xx/5xx). Nil only when a network or middleware error occurs. |
| `error` | Non-nil when no response could be obtained (network failure, timeout, circuit open, context cancellation, etc.). |

> **Warning:** A non-nil `*relay.Response` does not indicate HTTP success. Always check `resp.IsSuccess()` or inspect `resp.StatusCode` before treating the response as valid. relay does not automatically error on 4xx or 5xx by default.

### Example - Basic execute

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    req := client.Get("/users")
    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        log.Fatalf("request failed: %v", err)
    }
    defer resp.Body.Close()

    fmt.Println("status:", resp.StatusCode)
}
```

### Example - Execute with timeout context

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
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    req := client.Post("/jobs").
        WithBody(map[string]string{"type": "report"})

    resp, err := client.Execute(ctx, req)
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            log.Println("request timed out")
            return
        }
        log.Fatalf("unexpected error: %v", err)
    }
    defer resp.Body.Close()

    fmt.Println("job accepted:", resp.StatusCode)
}
```

---

## client.Get

```go
func (c *Client) Get(path string) *relay.Request
```

`Get` creates a new `*relay.Request` configured for the HTTP GET method. The `path` is appended to the client's base URL. The returned request is a builder - call additional methods on it to add headers, query parameters, or timeouts before passing it to `Execute`.

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `path` | `string` | The URL path, appended to the configured `BaseURL`. Can include path parameters that you format beforehand. |

### Return Values

Returns a `*relay.Request` builder pre-configured with method `GET`.

> **Tip:** For full URL control, pass an absolute URL as `path` and omit `WithBaseURL` on the client.

### Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    userID := "u-42"
    resp, err := client.Execute(
        context.Background(),
        client.Get("/users/"+userID).
            WithHeader("X-Request-ID", "abc-123").
            WithQueryParam("expand", "profile"),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    fmt.Println(resp.StatusCode)
}
```

---

## client.Post

```go
func (c *Client) Post(path string) *relay.Request
```

`Post` creates a new `*relay.Request` configured for the HTTP POST method. Use `WithBody` on the returned builder to attach a request body.

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `path` | `string` | The URL path appended to the base URL. |

### Return Values

Returns a `*relay.Request` builder pre-configured with method `POST`.

### Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type CreateUserRequest struct {
    Name  string `json:"name"`
    Email string `json:"email"`
}

type User struct {
    ID    string `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    payload := CreateUserRequest{Name: "Alice", Email: "alice@example.com"}

    resp, err := client.Execute(
        context.Background(),
        client.Post("/users").WithBody(payload),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    user, err := relay.DecodeJSON[User](resp)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("created user:", user.ID)
}
```

---

## client.Put

```go
func (c *Client) Put(path string) *relay.Request
```

`Put` creates a new `*relay.Request` for the HTTP PUT method, typically used for full resource replacement.

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `path` | `string` | The URL path appended to the base URL. |

### Return Values

Returns a `*relay.Request` builder pre-configured with method `PUT`.

### Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type UpdateProfileRequest struct {
    Name     string `json:"name"`
    Bio      string `json:"bio"`
    Location string `json:"location"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    resp, err := client.Execute(
        context.Background(),
        client.Put("/users/u-42/profile").WithBody(UpdateProfileRequest{
            Name:     "Alice Smith",
            Bio:      "Engineer",
            Location: "San Francisco",
        }),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    fmt.Println("updated, status:", resp.StatusCode)
}
```

---

## client.Patch

```go
func (c *Client) Patch(path string) *relay.Request
```

`Patch` creates a new `*relay.Request` for the HTTP PATCH method, used for partial resource updates.

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `path` | `string` | The URL path appended to the base URL. |

### Return Values

Returns a `*relay.Request` builder pre-configured with method `PATCH`.

### Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    resp, err := client.Execute(
        context.Background(),
        client.Patch("/users/u-42").WithBody(map[string]string{
            "email": "newalice@example.com",
        }),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    fmt.Println("patch status:", resp.StatusCode)
}
```

---

## client.Delete

```go
func (c *Client) Delete(path string) *relay.Request
```

`Delete` creates a new `*relay.Request` for the HTTP DELETE method.

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `path` | `string` | The URL path appended to the base URL. |

### Return Values

Returns a `*relay.Request` builder pre-configured with method `DELETE`.

### Example

```go
package main

import (
    "context"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    resp, err := client.Execute(
        context.Background(),
        client.Delete("/users/u-42"),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != 204 {
        log.Printf("unexpected status: %d", resp.StatusCode)
    }
}
```

---

## client.Head

```go
func (c *Client) Head(path string) *relay.Request
```

`Head` creates a new `*relay.Request` for the HTTP HEAD method. The server returns only headers - the response body will be empty. Useful for checking resource existence or retrieving metadata without downloading the body.

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `path` | `string` | The URL path appended to the base URL. |

### Return Values

Returns a `*relay.Request` builder pre-configured with method `HEAD`.

### Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    resp, err := client.Execute(context.Background(), client.Head("/users/u-42"))
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    fmt.Println("exists:", resp.StatusCode == 200)
    fmt.Println("content-type:", resp.Header.Get("Content-Type"))
}
```

---

## client.ExecuteWebSocket

```go
func (c *Client) ExecuteWebSocket(ctx context.Context, req *relay.Request) (*relay.WSConn, error)
```

`ExecuteWebSocket` upgrades the HTTP connection to a WebSocket connection and returns a `*relay.WSConn` for full-duplex communication. The request path should point to a WebSocket-capable endpoint. The underlying HTTP client handles the upgrade handshake.

> **Note:** WebSocket support was introduced in v0.1.14. The relay client must not be configured with transport adapters that do not support connection hijacking.

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `ctx` | `context.Context` | Controls the lifetime of the WebSocket connection. Cancelling the context closes the connection. |
| `req` | `*relay.Request` | The request specifying the WebSocket endpoint. Use `client.Get` to create the initial request. |

### Return Values

| Value | Description |
|-------|-------------|
| `*relay.WSConn` | An active WebSocket connection. The caller is responsible for closing it. |
| `error` | Non-nil if the upgrade failed or the context was already cancelled. |

### Example

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
    client := relay.New(relay.WithBaseURL("wss://api.example.com"))

    ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
    defer cancel()

    conn, err := client.ExecuteWebSocket(ctx, client.Get("/ws/events"))
    if err != nil {
        log.Fatalf("websocket upgrade failed: %v", err)
    }
    defer conn.Close()

    // Send a subscription message
    if err := conn.WriteJSON(map[string]string{"action": "subscribe", "channel": "orders"}); err != nil {
        log.Fatalf("write failed: %v", err)
    }

    // Read messages in a loop
    for {
        var msg map[string]interface{}
        if err := conn.ReadJSON(&msg); err != nil {
            log.Println("read error:", err)
            break
        }
        fmt.Printf("received: %v\n", msg)
    }
}
```

---

## relay.Paginate

```go
func Paginate[T any](ctx context.Context, client *relay.Client, req *relay.Request, fn relay.PageFunc[T]) ([]T, error)
```

`Paginate` is a generic helper that automatically follows paginated API responses. It calls `fn` on each page of results, accumulating all items across pages into a single slice. Pagination is driven by the `Link` header (RFC 5988 `rel="next"`).

### Type Parameters

| Parameter | Constraint | Description |
|-----------|------------|-------------|
| `T` | `any` | The type of each item in the paginated response. |

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `ctx` | `context.Context` | Controls the entire pagination loop. Cancelling it stops after the current page. |
| `client` | `*relay.Client` | The client to use for each page request. |
| `req` | `*relay.Request` | The initial request for the first page. |
| `fn` | `relay.PageFunc[T]` | A function that decodes one page's response into `[]T`. |

### Return Values

| Value | Description |
|-------|-------------|
| `[]T` | All accumulated items across all pages. |
| `error` | Non-nil if any page request fails or the context is cancelled. |

### relay.PageFunc type

```go
type PageFunc[T any] func(resp *relay.Response) ([]T, error)
```

### Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type Repository struct {
    ID       int    `json:"id"`
    FullName string `json:"full_name"`
    Stars    int    `json:"stargazers_count"`
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.github.com"),
        relay.WithBearerToken("ghp_your_token_here"),
        relay.WithDefaultAccept("application/vnd.github+json"),
    )

    req := client.Get("/user/repos").
        WithQueryParam("per_page", "100").
        WithQueryParam("sort", "updated")

    repos, err := relay.Paginate[Repository](
        context.Background(),
        client,
        req,
        func(resp *relay.Response) ([]Repository, error) {
            return relay.DecodeJSON[[]Repository](resp)
        },
    )
    if err != nil {
        log.Fatalf("pagination failed: %v", err)
    }

    fmt.Printf("total repositories: %d\n", len(repos))
    for _, r := range repos {
        fmt.Printf("  %s (stars: %d)\n", r.FullName, r.Stars)
    }
}
```

---

## Putting It All Together

The following example demonstrates a realistic usage pattern combining several client methods:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

type Order struct {
    ID     string  `json:"id"`
    Item   string  `json:"item"`
    Amount float64 `json:"amount"`
    Status string  `json:"status"`
}

type CreateOrderRequest struct {
    Item   string  `json:"item"`
    Amount float64 `json:"amount"`
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://orders.example.com"),
        relay.WithBearerToken("bearer-token"),
        relay.WithTimeout(10*time.Second),
        relay.WithRetry(&relay.RetryConfig{MaxAttempts: 3}),
    )

    ctx := context.Background()

    // Create an order
    resp, err := client.Execute(ctx, client.Post("/v1/orders").
        WithBody(CreateOrderRequest{Item: "widget", Amount: 29.99}))
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    order, err := relay.DecodeJSON[Order](resp)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("created:", order.ID)

    // Fetch all orders via pagination
    allOrders, err := relay.Paginate[Order](
        ctx, client,
        client.Get("/v1/orders").WithQueryParam("page_size", "50"),
        func(r *relay.Response) ([]Order, error) {
            return relay.DecodeJSON[[]Order](r)
        },
    )
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("total orders:", len(allOrders))

    // Delete the created order
    del, err := client.Execute(ctx, client.Delete("/v1/orders/"+order.ID))
    if err != nil {
        log.Fatal(err)
    }
    defer del.Body.Close()
    fmt.Println("deleted, status:", del.StatusCode)
}
```
