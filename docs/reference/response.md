# Response API Reference

`relay.Response` wraps the standard `*http.Response` and adds convenience methods for body consumption, status checking, and redirect chain inspection. It is returned by `client.Execute` when a network round-trip succeeds - even if the HTTP status code indicates an error (4xx, 5xx).

---

## relay.Response Struct

```go
type Response struct {
    StatusCode int
    Header     http.Header
    Body       io.ReadCloser
    // unexported fields
}
```

The struct has three exported fields and several unexported fields supporting the convenience methods. You never construct a `relay.Response` directly; it is always returned by `client.Execute`.

> **Warning:** Always close `resp.Body` when you are done with the response, even if you do not read the body. Failing to close the body will leak the underlying TCP connection back to the pool, eventually exhausting the connection limit.

```go
resp, err := client.Execute(ctx, req)
if err != nil {
    return err
}
defer resp.Body.Close() // Always defer this immediately
```

---

## resp.StatusCode

```go
StatusCode int
```

`StatusCode` holds the HTTP response status code (e.g., 200, 201, 404, 500). It is set directly from the underlying `http.Response.StatusCode`.

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

    resp, err := client.Execute(context.Background(), client.Get("/users/u-99"))
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    switch resp.StatusCode {
    case 200:
        fmt.Println("user found")
    case 404:
        fmt.Println("user not found")
    case 429:
        fmt.Println("rate limited")
    default:
        fmt.Printf("unexpected status: %d\n", resp.StatusCode)
    }
}
```

---

## resp.Header

```go
Header http.Header
```

`Header` contains the HTTP response headers as a `http.Header` (a `map[string][]string`). Header names are canonicalized. Use the standard `Header.Get(key)` method for case-insensitive single-value access.

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

    resp, err := client.Execute(context.Background(), client.Get("/users"))
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    // Single value access
    contentType := resp.Header.Get("Content-Type")
    requestID := resp.Header.Get("X-Request-ID")
    rateLimit := resp.Header.Get("X-RateLimit-Remaining")

    fmt.Println("content-type:", contentType)
    fmt.Println("request-id:", requestID)
    fmt.Println("rate-limit-remaining:", rateLimit)

    // Multi-value header access (e.g., Set-Cookie, Vary)
    for _, cookie := range resp.Header["Set-Cookie"] {
        fmt.Println("cookie:", cookie)
    }

    // Check for a Link header (pagination)
    if link := resp.Header.Get("Link"); link != "" {
        fmt.Println("pagination link:", link)
    }
}
```

---

## resp.Body

```go
Body io.ReadCloser
```

`Body` is the raw response body as an `io.ReadCloser`. For small responses, prefer `resp.Text()` or `resp.Bytes()` which handle reading and closing. For large streaming responses (file downloads, chunked streams), read directly from `Body`.

> **Note:** `Body` can only be read once. After `resp.Text()`, `resp.Bytes()`, or any decoder call, the body is exhausted. Attempting to read it again returns `io.EOF`.

### Example - Streaming a large response

```go
package main

import (
    "context"
    "io"
    "log"
    "os"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://files.example.com"))

    resp, err := client.Execute(
        context.Background(),
        client.Get("/exports/large-dataset.csv"),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    if !resp.IsSuccess() {
        log.Fatalf("download failed: %d", resp.StatusCode)
    }

    // Stream to a file without loading the entire body into memory
    f, err := os.Create("dataset.csv")
    if err != nil {
        log.Fatal(err)
    }
    defer f.Close()

    written, err := io.Copy(f, resp.Body)
    if err != nil {
        log.Fatal("stream error:", err)
    }
    log.Printf("downloaded %d bytes", written)
}
```

---

## resp.Text

```go
func (r *Response) Text() (string, error)
```

`Text` reads the entire response body, closes it, and returns the content as a string. This is a convenience wrapper around `resp.Bytes()` with a string conversion.

### Return Values

| Value | Description |
|-------|-------------|
| `string` | The response body as a UTF-8 string. Empty string on error. |
| `error` | Non-nil if reading the body fails. |

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

    resp, err := client.Execute(context.Background(), client.Get("/version"))
    if err != nil {
        log.Fatal(err)
    }

    body, err := resp.Text()
    if err != nil {
        log.Fatal("read error:", err)
    }

    // Body is already closed after Text()
    fmt.Println("version:", body)
}
```

> **Tip:** Use `resp.Text()` for debugging or when working with plain text APIs. For JSON or XML responses, prefer `relay.DecodeJSON` or `relay.DecodeAs` which are more efficient (they parse the body in a single pass without a string intermediate).

---

## resp.Bytes

```go
func (r *Response) Bytes() ([]byte, error)
```

`Bytes` reads the entire response body into a byte slice and closes the body. The returned slice can be inspected or passed to custom decoders.

### Return Values

| Value | Description |
|-------|-------------|
| `[]byte` | The full response body. Nil on error. |
| `error` | Non-nil if reading fails. |

### Example

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    resp, err := client.Execute(context.Background(), client.Get("/config"))
    if err != nil {
        log.Fatal(err)
    }

    data, err := resp.Bytes()
    if err != nil {
        log.Fatal("read error:", err)
    }

    // Use data with any decoder
    var config map[string]interface{}
    if err := json.Unmarshal(data, &config); err != nil {
        log.Fatal("parse error:", err)
    }
    fmt.Println("config:", config)
}
```

---

## resp.ContentType

```go
func (r *Response) ContentType() string
```

`ContentType` returns the value of the `Content-Type` response header, stripping any parameters (e.g., `; charset=utf-8`). The returned value is always lowercase.

### Return Values

Returns the base media type string, e.g., `"application/json"`, `"text/html"`, `"application/xml"`. Returns `""` if the header is absent.

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

    resp, err := client.Execute(context.Background(), client.Get("/data"))
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    ct := resp.ContentType()
    fmt.Println("content type:", ct)

    switch ct {
    case "application/json":
        fmt.Println("decoding JSON")
    case "application/xml", "text/xml":
        fmt.Println("decoding XML")
    case "text/csv":
        fmt.Println("parsing CSV")
    default:
        fmt.Println("unknown content type:", ct)
    }
}
```

---

## resp.IsSuccess

```go
func (r *Response) IsSuccess() bool
```

`IsSuccess` returns `true` if the HTTP status code is in the 2xx range (200-299), indicating the request was fulfilled successfully.

### Return Values

Returns `bool`.

### Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type CreateResult struct {
    ID      string `json:"id"`
    Created bool   `json:"created"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    resp, err := client.Execute(
        context.Background(),
        client.Post("/items").WithBody(map[string]string{"name": "widget"}),
    )
    if err != nil {
        log.Fatal(err)
    }

    if !resp.IsSuccess() {
        body, _ := resp.Text()
        log.Fatalf("create failed: %d - %s", resp.StatusCode, body)
    }

    result, err := relay.DecodeJSON[CreateResult](resp)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("created item:", result.ID)
}
```

---

## resp.IsError

```go
func (r *Response) IsError() bool
```

`IsError` returns `true` if the HTTP status code is in the 4xx or 5xx range, indicating a client or server error.

### Return Values

Returns `bool`.

> **Tip:** Combining `resp.IsError()` with `relay.DecodeJSON` lets you decode error response bodies into structured error types, which is common in REST APIs that return JSON error objects.

### Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type APIError struct {
    Code    string `json:"code"`
    Message string `json:"message"`
    Details []struct {
        Field   string `json:"field"`
        Issue   string `json:"issue"`
    } `json:"details"`
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    resp, err := client.Execute(
        context.Background(),
        client.Post("/orders").WithBody(map[string]interface{}{
            "item": "",  // intentionally invalid
        }),
    )
    if err != nil {
        log.Fatal(err)
    }

    if resp.IsError() {
        apiErr, decErr := relay.DecodeJSON[APIError](resp)
        if decErr != nil {
            log.Fatalf("HTTP %d (could not decode error body)", resp.StatusCode)
        }
        log.Fatalf("API error [%s]: %s", apiErr.Code, apiErr.Message)
    }
    defer resp.Body.Close()

    fmt.Println("order created, status:", resp.StatusCode)
}
```

---

## resp.RedirectChain

```go
func (r *Response) RedirectChain() []relay.RedirectInfo
```

`RedirectChain` returns the sequence of HTTP redirects followed before arriving at the final response. If no redirects were followed, the slice is empty. The final response itself is not included in the chain - only intermediate redirects.

### Return Values

Returns `[]relay.RedirectInfo`. Each element represents one redirect hop.

---

## relay.RedirectInfo Struct

```go
type RedirectInfo struct {
    URL        string
    StatusCode int
    Header     http.Header
}
```

| Field | Type | Description |
|-------|------|-------------|
| `URL` | `string` | The URL that was redirected to. |
| `StatusCode` | `int` | The redirect status code (301, 302, 303, 307, 308). |
| `Header` | `http.Header` | Response headers from the redirect response. |

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
    client := relay.New(
        relay.WithBaseURL("https://example.com"),
        relay.WithMaxRedirects(5),
    )

    resp, err := client.Execute(
        context.Background(),
        client.Get("/old-path"),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    chain := resp.RedirectChain()
    if len(chain) > 0 {
        fmt.Printf("followed %d redirect(s):\n", len(chain))
        for i, r := range chain {
            fmt.Printf("  %d. [%d] %s\n", i+1, r.StatusCode, r.URL)
        }
    }

    fmt.Println("final URL:", resp.Header.Get("X-Final-URL"))
    fmt.Println("final status:", resp.StatusCode)
}
```

---

## Complete Response Handling Example

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

type Product struct {
    ID    string  `json:"id"`
    Name  string  `json:"name"`
    Price float64 `json:"price"`
    Stock int     `json:"stock"`
}

type APIError struct {
    Code    string `json:"error_code"`
    Message string `json:"message"`
}

func fetchProduct(client *relay.Client, id string) (*Product, error) {
    resp, err := client.Execute(context.Background(), client.Get("/products/"+id))
    if err != nil {
        if relay.IsTimeout(err) {
            return nil, fmt.Errorf("product fetch timed out for %s", id)
        }
        return nil, fmt.Errorf("network error: %w", err)
    }

    // Log redirect chain if any
    if chain := resp.RedirectChain(); len(chain) > 0 {
        log.Printf("followed %d redirect(s) to reach product %s", len(chain), id)
    }

    // Handle 404 explicitly
    if resp.StatusCode == 404 {
        resp.Body.Close()
        return nil, fmt.Errorf("product %s not found", id)
    }

    // Handle other errors
    if resp.IsError() {
        apiErr, decErr := relay.DecodeJSON[APIError](resp)
        if decErr != nil {
            return nil, fmt.Errorf("HTTP %d for product %s", resp.StatusCode, id)
        }
        return nil, fmt.Errorf("API error [%s]: %s", apiErr.Code, apiErr.Message)
    }

    // Validate content type
    if resp.ContentType() != "application/json" {
        resp.Body.Close()
        return nil, fmt.Errorf("unexpected content type: %s", resp.ContentType())
    }

    // Decode success response
    product, err := relay.DecodeJSON[Product](resp)
    if err != nil {
        return nil, fmt.Errorf("decode error: %w", err)
    }
    return &product, nil
}

func main() {
    client := relay.New(relay.WithBaseURL("https://catalog.example.com"))

    product, err := fetchProduct(client, "prod-001")
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            log.Fatal("deadline exceeded")
        }
        log.Fatal("error:", err)
    }
    fmt.Printf("product: %s, price: %.2f, stock: %d\n",
        product.Name, product.Price, product.Stock)
}
```
