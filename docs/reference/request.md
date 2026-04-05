# Request Builder API Reference

The `relay.Request` type is an immutable-style builder for constructing HTTP requests. You never create a `relay.Request` directly; instead, you obtain one from the client helper methods (`client.Get`, `client.Post`, `client.Put`, `client.Patch`, `client.Delete`, `client.Head`) and then call chained methods to configure it before passing it to `client.Execute`.

All builder methods return `*relay.Request` to enable method chaining. The original request is not mutated; each method returns a new derived request, making it safe to branch from a base request without sharing state between branches.

---

## relay.Request Struct

`relay.Request` is an opaque struct. Its internal fields are not exported. All access is through the builder and accessor methods described in this reference.

```go
// relay.Request is obtained from client methods, never constructed directly.
// The zero value is not usable.
type Request struct {
    // unexported fields
}
```

### Creating a Request

```go
package main

import (
    "context"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    // Create a base GET request
    req := client.Get("/users")

    // Execute it immediately (no additional configuration)
    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
}
```

### Shared base request pattern

Because builder methods return new request values, you can safely derive multiple specific requests from a common base:

```go
package main

import (
    "context"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    // Base request with shared headers
    base := client.Get("/users").
        WithHeader("X-Service", "billing").
        WithHeader("Accept-Language", "en-US")

    // Branch 1: active users
    activeReq := base.WithQueryParam("status", "active")

    // Branch 2: suspended users - base is unchanged
    suspendedReq := base.WithQueryParam("status", "suspended")

    resp1, err := client.Execute(context.Background(), activeReq)
    if err != nil {
        log.Fatal(err)
    }
    defer resp1.Body.Close()

    resp2, err := client.Execute(context.Background(), suspendedReq)
    if err != nil {
        log.Fatal(err)
    }
    defer resp2.Body.Close()
}
```

---

## req.WithHeader

```go
func (r *Request) WithHeader(key, value string) *relay.Request
```

`WithHeader` adds or replaces an HTTP header on the request. Header names are canonicalized using `http.CanonicalHeaderKey`. Calling `WithHeader` with the same key multiple times replaces the previous value rather than appending.

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `key` | `string` | The header name (e.g., `"Content-Type"`, `"X-Request-ID"`). Case-insensitive. |
| `value` | `string` | The header value to set. |

### Return Values

Returns a new `*relay.Request` with the header applied. The original request is unchanged.

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

    req := client.Post("/events").
        WithHeader("Content-Type", "application/json").
        WithHeader("X-Request-ID", "req-9f3a1c").
        WithHeader("X-Idempotency-Key", "idem-abc123").
        WithBody(map[string]string{"event": "user.signup", "user_id": "u-42"})

    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    log.Println("status:", resp.StatusCode)
}
```

> **Tip:** Headers set via `WithHeader` on a request take precedence over default headers set by client-level options like `WithBearerToken` and `WithAPIKey`, giving you per-request override capability.

---

## req.WithQueryParam

```go
func (r *Request) WithQueryParam(key, value string) *relay.Request
```

`WithQueryParam` appends a query parameter to the request URL. Multiple calls with the same key append additional values (resulting in repeated query parameters, e.g., `?tag=go&tag=http`). Use this method instead of embedding query strings in the path to ensure correct URL encoding.

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `key` | `string` | The query parameter name. URL-encoded automatically. |
| `value` | `string` | The query parameter value. URL-encoded automatically. |

### Return Values

Returns a new `*relay.Request` with the query parameter appended.

### Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "strconv"

    "github.com/jhonsferg/relay"
)

type SearchResult struct {
    Items []struct {
        ID    string `json:"id"`
        Name  string `json:"name"`
        Score float64 `json:"score"`
    } `json:"items"`
    Total int `json:"total"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    req := client.Get("/search").
        WithQueryParam("q", "golang http client").
        WithQueryParam("page", strconv.Itoa(1)).
        WithQueryParam("per_page", "25").
        WithQueryParam("tag", "networking").
        WithQueryParam("tag", "http")

    // Resulting URL: /search?q=golang+http+client&page=1&per_page=25&tag=networking&tag=http

    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    results, err := relay.DecodeJSON[SearchResult](resp)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("found %d results\n", results.Total)
}
```

---

## req.WithBody

```go
func (r *Request) WithBody(body interface{}) *relay.Request
```

`WithBody` sets the request body. By default, relay serializes the value to JSON and sets `Content-Type: application/json`. If a custom encoder is registered via `WithContentTypeEncoder` and the request's `Content-Type` header matches, the custom encoder is used instead.

Supported body types:
- Any struct or map - JSON-encoded
- `[]byte` - sent as-is with `Content-Type: application/octet-stream` if no Content-Type is set
- `string` - sent as-is with `Content-Type: text/plain` if no Content-Type is set
- `io.Reader` - streamed directly; `Content-Type` must be set manually

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `body` | `interface{}` | The request body. Structs and maps are JSON-encoded. |

### Return Values

Returns a new `*relay.Request` with the body configured.

### Example - Struct body

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type Invoice struct {
    CustomerID string   `json:"customer_id"`
    Items      []string `json:"items"`
    Currency   string   `json:"currency"`
}

type InvoiceResponse struct {
    ID     string `json:"id"`
    Status string `json:"status"`
    Total  int    `json:"total_cents"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://billing.example.com"))

    resp, err := client.Execute(
        context.Background(),
        client.Post("/invoices").WithBody(Invoice{
            CustomerID: "cust-001",
            Items:      []string{"item-a", "item-b"},
            Currency:   "USD",
        }),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    invoice, err := relay.DecodeJSON[InvoiceResponse](resp)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("invoice %s created, total: %d cents\n", invoice.ID, invoice.Total)
}
```

### Example - Raw bytes with custom content type

```go
package main

import (
    "context"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    rawCSV := []byte("name,email\nAlice,alice@example.com\nBob,bob@example.com")

    resp, err := client.Execute(
        context.Background(),
        client.Post("/import").
            WithHeader("Content-Type", "text/csv").
            WithBody(rawCSV),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    log.Println("import status:", resp.StatusCode)
}
```

---

## req.WithContext

```go
func (r *Request) WithContext(ctx context.Context) *relay.Request
```

`WithContext` attaches a `context.Context` to the request, overriding any context that will be passed to `client.Execute`. This is useful when building requests ahead of time that should carry their own context, or when constructing requests in middleware that need to propagate context values.

In most cases, pass the context directly to `client.Execute` instead of using `WithContext` - that is the idiomatic pattern. Use `WithContext` only when you need to embed the context in the request object itself.

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `ctx` | `context.Context` | The context to attach. Must not be nil. |

### Return Values

Returns a new `*relay.Request` with the context attached.

### Example

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func buildRequest(client *relay.Client) *relay.Request {
    // Build a request with an embedded context
    ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
    return client.Get("/status").WithContext(ctx)
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    req := buildRequest(client)

    // The context embedded in req takes effect here
    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
}
```

---

## req.WithTimeout

```go
func (r *Request) WithTimeout(d time.Duration) *relay.Request
```

`WithTimeout` sets a per-request timeout that overrides the client-level timeout configured with `relay.WithTimeout`. Use this when specific requests require a different deadline than the client default - for example, a long-running report endpoint that needs more time, or a health check that must fail fast.

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `d` | `time.Duration` | The timeout duration for this specific request. |

### Return Values

Returns a new `*relay.Request` with the per-request timeout set.

> **Note:** The per-request timeout applies per attempt, not across all retry attempts. A request configured with a 5-second timeout and 3 retries may take up to 15 seconds plus backoff time in the worst case.

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
    // Client has a generous default timeout
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTimeout(30*time.Second),
    )

    ctx := context.Background()

    // Health check must respond quickly
    healthReq := client.Get("/health").WithTimeout(2 * time.Second)
    resp, err := client.Execute(ctx, healthReq)
    if err != nil {
        log.Println("health check failed:", err)
        return
    }
    defer resp.Body.Close()
    fmt.Println("health:", resp.StatusCode)

    // Report generation can take longer
    reportReq := client.Post("/reports/generate").
        WithTimeout(120*time.Second).
        WithBody(map[string]string{"type": "monthly", "month": "2024-01"})

    reportResp, err := client.Execute(ctx, reportReq)
    if err != nil {
        log.Fatal("report failed:", err)
    }
    defer reportResp.Body.Close()
    fmt.Println("report status:", reportResp.StatusCode)
}
```

---

## req.WithRetry

```go
func (r *Request) WithRetry(config *relay.RetryConfig) *relay.Request
```

`WithRetry` overrides the client-level retry configuration for this specific request. This allows fine-grained control: some endpoints may tolerate aggressive retries (read-heavy, idempotent) while others should fail fast (payment processing, state-mutating calls without idempotency).

### Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `config` | `*relay.RetryConfig` | Per-request retry configuration. Pass `nil` or a config with `MaxAttempts: 1` to disable retries for this request. |

### Return Values

Returns a new `*relay.Request` with the retry override applied.

### relay.RetryConfig fields

| Field | Type | Description |
|-------|------|-------------|
| `MaxAttempts` | `int` | Total attempts (1 = no retries). |
| `WaitMin` | `time.Duration` | Minimum backoff between attempts. |
| `WaitMax` | `time.Duration` | Maximum backoff. |
| `RetryOn` | `func(error) bool` | Custom retry predicate. |

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
    // Client has retries enabled by default
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithRetry(&relay.RetryConfig{
            MaxAttempts: 3,
            WaitMin:     100 * time.Millisecond,
            WaitMax:     1 * time.Second,
        }),
    )

    ctx := context.Background()

    // This payment request should NOT be retried automatically
    payReq := client.Post("/payments").
        WithRetry(&relay.RetryConfig{MaxAttempts: 1}).
        WithBody(map[string]interface{}{
            "amount":   5000,
            "currency": "USD",
            "card":     "tok_visa",
        })

    resp, err := client.Execute(ctx, payReq)
    if err != nil {
        log.Fatal("payment error:", err)
    }
    defer resp.Body.Close()
    fmt.Println("payment status:", resp.StatusCode)
}
```

---

## req.Method

```go
func (r *Request) Method() string
```

`Method` returns the HTTP method of the request as an uppercase string (e.g., `"GET"`, `"POST"`, `"DELETE"`).

### Return Values

Returns the HTTP method string.

### Example

```go
package main

import (
    "fmt"

    "github.com/jhonsferg/relay"
)

func logRequest(req *relay.Request) {
    fmt.Printf("[%s] %s\n", req.Method(), req.URL())
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    getReq := client.Get("/users")
    postReq := client.Post("/users")

    logRequest(getReq)  // [GET] https://api.example.com/users
    logRequest(postReq) // [POST] https://api.example.com/users
}
```

---

## req.URL

```go
func (r *Request) URL() string
```

`URL` returns the fully resolved URL of the request, including the base URL, path, and any query parameters added via `WithQueryParam`. This is the exact URL that will be sent when `client.Execute` is called.

### Return Values

Returns the full URL as a string.

### Example

```go
package main

import (
    "fmt"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    req := client.Get("/users").
        WithQueryParam("status", "active").
        WithQueryParam("page", "2")

    fmt.Println(req.URL())
    // Output: https://api.example.com/users?status=active&page=2
}
```

---

## req.Headers

```go
func (r *Request) Headers() http.Header
```

`Headers` returns a copy of the HTTP headers currently configured on the request. This includes headers set via `WithHeader` as well as any headers injected by client-level options (Bearer token, API key, etc.) at request construction time.

> **Note:** The returned `http.Header` is a copy. Modifying it does not affect the request. To add or change headers, use `WithHeader`.

### Return Values

Returns `http.Header` (a `map[string][]string`) containing all configured headers.

### Example

```go
package main

import (
    "fmt"
    "net/http"

    "github.com/jhonsferg/relay"
)

func validateRequest(req *relay.Request) error {
    headers := req.Headers()
    if headers.Get("Authorization") == "" {
        return fmt.Errorf("request to %s is missing Authorization header", req.URL())
    }
    return nil
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithBearerToken("token-xyz"),
    )

    req := client.Get("/secure-resource").
        WithHeader("X-Trace-ID", "trace-001")

    // Inspect headers before execution
    headers := req.Headers()
    for name, values := range headers {
        fmt.Printf("%s: %v\n", name, values)
    }

    if err := validateRequest(req); err != nil {
        fmt.Println("validation error:", err)
    }

    // Use http.Header methods
    ct := http.Header(headers).Get("Content-Type")
    fmt.Println("content-type:", ct)
}
```

---

## Method Chaining Patterns

The builder API is designed for fluent chaining. All `With*` methods can be combined in a single expression:

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
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    type CreateWebhook struct {
        URL    string   `json:"url"`
        Events []string `json:"events"`
        Secret string   `json:"secret"`
    }

    req := client.Post("/webhooks").
        WithHeader("X-Request-ID", "req-abc-789").
        WithHeader("X-Idempotency-Key", "idem-xyz-456").
        WithTimeout(5*time.Second).
        WithRetry(&relay.RetryConfig{MaxAttempts: 2}).
        WithBody(CreateWebhook{
            URL:    "https://myservice.example.com/hooks/relay",
            Events: []string{"order.created", "order.shipped"},
            Secret: "wh-secret-value",
        })

    fmt.Printf("built %s request to %s\n", req.Method(), req.URL())

    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```
