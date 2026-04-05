# Basic Requests

This guide covers everything you need to make HTTP requests with relay - from simple GETs to multipart file uploads, and from reading raw bytes to checking response success.

## Creating a Client

Every relay interaction starts with a `*relay.Client`. Configure it once and reuse it across your application.

```go
package main

import (
    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTimeout(30 * time.Second),
    )
    _ = client
}
```

## HTTP Methods

relay exposes a builder method for each standard HTTP verb. Each method returns a `*relay.Request` that you execute with `client.Execute`.

### GET

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTimeout(10 * time.Second),
    )

    req := client.Get("/users")
    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    fmt.Println("Status:", resp.StatusCode)
}
```

### POST

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/jhonsferg/relay"
)

type CreateUserRequest struct {
    Name  string `json:"name"`
    Email string `json:"email"`
}

type CreateUserResponse struct {
    ID    int    `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTimeout(10 * time.Second),
    )

    body := CreateUserRequest{Name: "Alice", Email: "alice@example.com"}
    req := client.Post("/users").WithBody(body)
    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    user, err := relay.DecodeJSON[CreateUserResponse](resp)
    if err != nil {
        panic(err)
    }
    fmt.Printf("Created user %d: %s\n", user.ID, user.Name)
}
```

### PUT

Use PUT to replace a resource entirely.

```go
req := client.Put("/users/42").WithBody(UpdateUserRequest{
    Name:  "Alice Smith",
    Email: "alice.smith@example.com",
})
resp, err := client.Execute(ctx, req)
```

### PATCH

Use PATCH for partial updates.

```go
type PatchRequest struct {
    Name string `json:"name,omitempty"`
}

req := client.Patch("/users/42").WithBody(PatchRequest{Name: "Alice Updated"})
resp, err := client.Execute(ctx, req)
```

### DELETE

```go
req := client.Delete("/users/42")
resp, err := client.Execute(ctx, req)
if err != nil {
    panic(err)
}
if resp.IsSuccess() {
    fmt.Println("User deleted")
}
```

### HEAD

HEAD requests are useful to check if a resource exists or to inspect headers without downloading a body.

```go
req := client.Head("/users/42")
resp, err := client.Execute(ctx, req)
if err != nil {
    panic(err)
}
fmt.Println("Content-Length:", resp.Header.Get("Content-Length"))
fmt.Println("Last-Modified:", resp.Header.Get("Last-Modified"))
```

## Base URL Configuration

Set the base URL once on the client; request paths are resolved relative to it.

```go
client := relay.New(
    relay.WithBaseURL("https://api.example.com/v2"),
)

// Resolves to https://api.example.com/v2/users
req := client.Get("/users")
```

!!! note "Trailing slashes"
    relay trims trailing slashes from the base URL and leading slashes from paths before joining them, so `WithBaseURL("https://example.com/v2/")` and paths like `"/users"` or `"users"` all work correctly.

## Adding Headers

Use `WithHeader` on the request builder to set individual headers.

```go
req := client.Get("/users").
    WithHeader("X-Request-ID", "abc-123").
    WithHeader("Accept-Language", "en-US")

resp, err := client.Execute(ctx, req)
```

To add headers to every request, use `WithDefaultHeader` on the client:

```go
client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relay.WithHeader("X-App-Version", "2.1.0"),
)
```

!!! tip "Header precedence"
    Per-request headers set via `req.WithHeader` override client-level headers with the same name.

## Query Parameters

Append query parameters with `WithQueryParam`. Multiple calls accumulate parameters.

```go
req := client.Get("/users").
    WithQueryParam("page", "2").
    WithQueryParam("per_page", "50").
    WithQueryParam("sort", "created_at")

// Resulting URL: /users?page=2&per_page=50&sort=created_at
resp, err := client.Execute(ctx, req)
```

For building query strings dynamically:

```go
params := map[string]string{
    "status": "active",
    "role":   "admin",
}

req := client.Get("/users")
for k, v := range params {
    req = req.WithQueryParam(k, v)
}
```

## Request Body

### JSON Body

Pass any Go value to `WithBody`. relay serializes it to JSON and sets `Content-Type: application/json` automatically.

```go
type Order struct {
    ProductID int    `json:"product_id"`
    Quantity  int    `json:"quantity"`
    Note      string `json:"note,omitempty"`
}

req := client.Post("/orders").WithBody(Order{
    ProductID: 101,
    Quantity:  3,
    Note:      "Gift wrap please",
})
```

### Form Data

To send `application/x-www-form-urlencoded` data, pass a `url.Values`:

```go
import "net/url"

form := url.Values{}
form.Set("username", "alice")
form.Set("password", "secret")

req := client.Post("/login").
    WithHeader("Content-Type", "application/x-www-form-urlencoded").
    WithBody(strings.NewReader(form.Encode()))
```

### Raw Bytes

For arbitrary binary payloads:

```go
data := []byte{0x89, 0x50, 0x4E, 0x47} // PNG magic bytes (example)

req := client.Post("/upload").
    WithHeader("Content-Type", "image/png").
    WithBody(bytes.NewReader(data))
```

### No Body

Methods like GET and DELETE should not have a body. Simply omit `WithBody`.

## File Uploads (Multipart Form)

Use the standard library's `multipart.Writer` to build a multipart request body:

```go
package main

import (
    "bytes"
    "context"
    "fmt"
    "mime/multipart"
    "os"

    "github.com/jhonsferg/relay"
)

func uploadFile(client *relay.Client, filePath string) error {
    fileData, err := os.ReadFile(filePath)
    if err != nil {
        return err
    }

    var buf bytes.Buffer
    mw := multipart.NewWriter(&buf)

    // Add a text field
    if err := mw.WriteField("description", "Profile photo"); err != nil {
        return err
    }

    // Add the file
    fw, err := mw.CreateFormFile("file", "photo.jpg")
    if err != nil {
        return err
    }
    if _, err := fw.Write(fileData); err != nil {
        return err
    }

    if err := mw.Close(); err != nil {
        return err
    }

    req := client.Post("/upload").
        WithHeader("Content-Type", mw.FormDataContentType()).
        WithBody(&buf)

    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    fmt.Println("Upload status:", resp.StatusCode)
    return nil
}
```

## Reading the Response

### Status Code

```go
resp, err := client.Execute(ctx, req)
if err != nil {
    panic(err)
}

fmt.Println("HTTP Status:", resp.StatusCode)
// e.g., 200, 201, 404, 500
```

### Body as String

```go
resp, err := client.Execute(ctx, req)
if err != nil {
    panic(err)
}

text, err := resp.Text()
if err != nil {
    panic(err)
}
fmt.Println(text)
```

!!! warning "Body can only be read once"
    `resp.Body` is an `io.ReadCloser`. After calling `resp.Text()` or `resp.Bytes()`, the body is consumed. Do not call them more than once, and always `defer resp.Body.Close()` when reading directly.

### Body as Bytes

```go
resp, err := client.Execute(ctx, req)
if err != nil {
    panic(err)
}

data, err := resp.Bytes()
if err != nil {
    panic(err)
}
fmt.Printf("Received %d bytes\n", len(data))
```

### Body as JSON

Use the generic `DecodeJSON[T]` helper:

```go
type Product struct {
    ID    int     `json:"id"`
    Name  string  `json:"name"`
    Price float64 `json:"price"`
}

resp, err := client.Execute(ctx, client.Get("/products/1"))
if err != nil {
    panic(err)
}

product, err := relay.DecodeJSON[Product](resp)
if err != nil {
    panic(err)
}
fmt.Printf("%s costs $%.2f\n", product.Name, product.Price)
```

## Success and Error Checks

`IsSuccess()` returns true for 2xx status codes. `IsError()` returns true for 4xx and 5xx.

```go
resp, err := client.Execute(ctx, req)
if err != nil {
    // Network error, timeout, circuit open, etc.
    panic(err)
}
defer resp.Body.Close()

if resp.IsSuccess() {
    fmt.Println("Request succeeded:", resp.StatusCode)
} else if resp.IsError() {
    body, _ := resp.Text()
    fmt.Printf("Request failed with %d: %s\n", resp.StatusCode, body)
}
```

!!! note "HTTP errors vs Go errors"
    A 404 or 500 response does NOT return a non-nil Go error - `err` will be `nil` but `resp.StatusCode` will be 404/500. Use `IsError()` to detect HTTP-level failures. See [Error Handling](../guides/error-handling.md) for the full picture.

## Path Parameters Pattern

relay does not have built-in path parameter interpolation. Use `fmt.Sprintf` to build paths:

```go
userID := 42
req := client.Get(fmt.Sprintf("/users/%d", userID))

// Or with string IDs
orgSlug := "acme-corp"
repoSlug := "my-app"
req = client.Get(fmt.Sprintf("/orgs/%s/repos/%s", orgSlug, repoSlug))
```

For more complex URL building, use `net/url`:

```go
import "net/url"

base, _ := url.Parse("https://api.example.com")
ref, _ := url.Parse(fmt.Sprintf("/users/%d/settings", userID))
fullURL := base.ResolveReference(ref).String()

req := client.Get(fullURL)
```

## Full Example: CRUD Operations

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/jhonsferg/relay"
)

type Article struct {
    ID      int    `json:"id"`
    Title   string `json:"title"`
    Content string `json:"content"`
}

type ArticleInput struct {
    Title   string `json:"title"`
    Content string `json:"content"`
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com/v1"),
        relay.WithTimeout(15 * time.Second),
        relay.WithBearerToken("my-secret-token"),
    )

    ctx := context.Background()

    // CREATE
    createReq := client.Post("/articles").WithBody(ArticleInput{
        Title:   "Hello, relay!",
        Content: "relay makes HTTP easy.",
    })
    createResp, err := client.Execute(ctx, createReq)
    if err != nil {
        panic(err)
    }
    article, _ := relay.DecodeJSON[Article](createResp)
    fmt.Printf("Created article %d: %s\n", article.ID, article.Title)

    // READ
    getReq := client.Get(fmt.Sprintf("/articles/%d", article.ID))
    getResp, err := client.Execute(ctx, getReq)
    if err != nil {
        panic(err)
    }
    fetched, _ := relay.DecodeJSON[Article](getResp)
    fmt.Printf("Fetched: %s\n", fetched.Title)

    // UPDATE
    patchReq := client.Patch(fmt.Sprintf("/articles/%d", article.ID)).
        WithBody(ArticleInput{Title: "Updated Title", Content: fetched.Content})
    _, err = client.Execute(ctx, patchReq)
    if err != nil {
        panic(err)
    }
    fmt.Println("Updated article")

    // DELETE
    deleteReq := client.Delete(fmt.Sprintf("/articles/%d", article.ID))
    deleteResp, err := client.Execute(ctx, deleteReq)
    if err != nil {
        panic(err)
    }
    if deleteResp.IsSuccess() {
        fmt.Println("Article deleted")
    }
}
```

## Response Headers

Access response headers directly via `resp.Header`:

```go
resp, err := client.Execute(ctx, req)
if err != nil {
    panic(err)
}

etag := resp.Header.Get("ETag")
contentType := resp.ContentType()
rateRemaining := resp.Header.Get("X-RateLimit-Remaining")

fmt.Printf("ETag: %s\n", etag)
fmt.Printf("Content-Type: %s\n", contentType)
fmt.Printf("Rate limit remaining: %s\n", rateRemaining)
```

## Context and Cancellation

Every request accepts a `context.Context`. Use it for deadlines and cancellation:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

req := client.Get("/slow-endpoint")
resp, err := client.Execute(ctx, req)
if err != nil {
    if errors.Is(err, context.DeadlineExceeded) {
        fmt.Println("Request timed out")
    }
    return
}
```

## Next Steps

- [Authentication](auth.md) - Add Bearer, Basic, or API Key auth
- [Retries](retries.md) - Automatic retry with exponential backoff
- [Error Handling](error-handling.md) - Classify and handle errors
- [Pagination](pagination.md) - Paginate through large result sets
