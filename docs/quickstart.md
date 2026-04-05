# Quick Start

This page walks you through four progressive examples, each building on the last. By the end you will have a working mental model of relay's core concepts: building a client, making requests, decoding responses, adding resilience, and managing concurrency.

> **Note:** All examples assume you have already run `go get github.com/jhonsferg/relay`. See [Installation](installation.md) if you have not done that yet.

---

## Example 1 - Minimal GET request

The simplest possible relay program: fetch a URL and print the HTTP status code.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    // relay.New() with no options produces a client backed by net/http defaults.
    // A base URL is optional for one-off requests; you can pass a full URL to
    // client.Get() instead.
    client := relay.New()

    // Build a GET request. Nothing is sent yet.
    req := client.Get("https://httpbin.org/get")

    // Execute sends the request and returns a *relay.Response or an error.
    // The context controls the request lifetime (deadline, cancellation).
    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        log.Fatalf("request failed: %v", err)
    }
    // relay.Response embeds *http.Response, so all standard fields are accessible.
    defer resp.Body.Close()

    fmt.Printf("Status: %d %s\n", resp.StatusCode, resp.Status)
}
```

### What this demonstrates

- `relay.New()` - create a zero-configuration client
- `client.Get(url)` - construct a request builder without sending it
- `client.Execute(ctx, req)` - send the request and receive a response
- The returned `*relay.Response` wraps `*http.Response`

**Expected output:**

```
Status: 200 200 OK
```

---

## Example 2 - POST with a JSON body and decode the response

Most real-world API calls send a JSON payload and decode a JSON response. relay provides first-class helpers for both.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

// CreatePostRequest is the payload we will send.
type CreatePostRequest struct {
    Title  string `json:"title"`
    Body   string `json:"body"`
    UserID int    `json:"userId"`
}

// Post is the response we expect from the API.
type Post struct {
    ID     int    `json:"id"`
    Title  string `json:"title"`
    Body   string `json:"body"`
    UserID int    `json:"userId"`
}

func main() {
    // Configure the client with a base URL so that all requests in this
    // program are rooted there. Individual request paths are appended to it.
    client := relay.New(
        relay.WithBaseURL("https://jsonplaceholder.typicode.com"),
    )

    // Construct the request payload.
    payload := CreatePostRequest{
        Title:  "relay is great",
        Body:   "I replaced 80 lines of net/http boilerplate with 5 lines.",
        UserID: 42,
    }

    // client.Post() accepts a path and an optional body.
    // relay.JSONBody() serialises the value to JSON and sets Content-Type.
    req := client.Post("/posts", relay.JSONBody(payload))

    // Execute the request.
    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        log.Fatalf("request failed: %v", err)
    }

    // relay.DecodeJSON is a generic helper that reads the body, closes it,
    // and unmarshals into the type parameter you specify.
    post, err := relay.DecodeJSON[Post](resp)
    if err != nil {
        log.Fatalf("decode failed: %v", err)
    }

    fmt.Printf("Created post #%d: %q\n", post.ID, post.Title)
}
```

### What this demonstrates

- `relay.WithBaseURL` - anchor all requests to a base address
- `relay.JSONBody(v)` - serialize a Go value as the request body
- `relay.DecodeJSON[T](resp)` - generic JSON decode that closes the body for you
- Separating request construction from execution (builder pattern)

**Expected output:**

```
Created post #101: "relay is great"
```

> **Note:** JSONPlaceholder does not persist data; it echoes back a fake ID of 101 for every POST. In production the API would return the real resource ID.

### Checking status codes

relay does not treat non-2xx responses as errors by default, giving you full control over status handling. Use `relay.EnsureSuccess` when you want an error on 4xx/5xx:

```go
resp, err := client.Execute(context.Background(), req)
if err != nil {
    log.Fatalf("transport error: %v", err)
}

// EnsureSuccess returns an *relay.HTTPError for any response with status >= 400.
if err := relay.EnsureSuccess(resp); err != nil {
    log.Fatalf("API error: %v", err)
}
```

Or attach it as a hook so every request in the client enforces it automatically:

```go
client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relay.WithAfterResponse(relay.EnsureSuccessHook()),
)
```

---

## Example 3 - Client with retries, circuit breaker, and bearer auth

Production services need resilience. This example configures automatic retries with exponential backoff, a circuit breaker that opens after repeated failures, and Bearer token authentication.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

// APIUser represents a user returned by the example API.
type APIUser struct {
    ID       int    `json:"id"`
    Username string `json:"username"`
    Email    string `json:"email"`
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),

        // WithBearerToken injects "Authorization: Bearer <token>" on every request.
        relay.WithBearerToken("super-secret-token"),

        // WithRetry configures automatic retry behaviour.
        // MaxAttempts=4 means the original attempt plus up to 3 retries.
        // The backoff starts at 100ms and doubles with each attempt.
        // Jitter of 0.2 adds up to 20% random variance to prevent thundering-herd.
        relay.WithRetry(&relay.RetryConfig{
            MaxAttempts:     4,
            InitialInterval: 100 * time.Millisecond,
            Multiplier:      2.0,
            Jitter:          0.2,
            // Retry on these status codes in addition to transport errors.
            RetryableStatuses: []int{429, 500, 502, 503, 504},
        }),

        // WithCircuitBreaker opens the circuit when 50% of requests in the
        // last 20-request sliding window fail. Once open, requests fail fast
        // for 10 seconds before the circuit enters half-open state to probe
        // recovery. If the probe succeeds, the circuit closes again.
        relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
            FailureThreshold:  0.5,
            WindowSize:        20,
            OpenTimeout:       10 * time.Second,
        }),

        // WithDefaultHeaders sets headers sent on every request.
        relay.WithDefaultHeaders(map[string]string{
            "Accept":     "application/json",
            "User-Agent": "my-service/1.0",
        }),

        // WithTimeout sets a per-request timeout that includes all retry attempts.
        // For a deadline that resets on each attempt use WithAttemptTimeout instead.
        relay.WithTimeout(30 * time.Second),
    )

    // Build the request with a path-level override: add a custom query param.
    req := client.Get("/users/42").
        WithQueryParam("expand", "profile")

    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        // err wraps the underlying cause. relay errors implement the standard
        // errors.Is / errors.As interface for structured inspection.
        log.Fatalf("request failed after retries: %v", err)
    }

    user, err := relay.DecodeJSON[APIUser](resp)
    if err != nil {
        log.Fatalf("decode failed: %v", err)
    }

    fmt.Printf("User: %s <%s>\n", user.Username, user.Email)
}
```

### What this demonstrates

- `relay.WithRetry` - exponential backoff with jitter and retryable status codes
- `relay.WithCircuitBreaker` - sliding-window failure detection and automatic recovery
- `relay.WithBearerToken` - inject `Authorization` header on every request
- `relay.WithDefaultHeaders` - baseline headers applied to the whole client
- `relay.WithTimeout` - overall request deadline across all retry attempts
- Method chaining on the request builder (`.WithQueryParam`)

### Observing retries with a hook

Add a `BeforeRetry` hook to log each retry attempt:

```go
relay.WithBeforeRetry(func(ctx context.Context, req *relay.Request, attempt int, err error) {
    log.Printf("retrying %s (attempt %d): %v", req.URL, attempt, err)
}),
```

### Testing with a mock server

When writing unit tests you do not want to hit a real API. Use the mock transport:

```go
package myservice_test

import (
    "context"
    "net/http"
    "testing"

    "github.com/jhonsferg/relay"
    "github.com/jhonsferg/relay/ext/mock"
)

func TestGetUser(t *testing.T) {
    transport := mock.NewTransport().
        Stub(mock.Request{Method: http.MethodGet, Path: "/users/42"}).
        Return(mock.Response{
            StatusCode: http.StatusOK,
            Body:       `{"id":42,"username":"alice","email":"alice@example.com"}`,
        })

    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTransport(transport),
    )

    resp, err := client.Execute(context.Background(), client.Get("/users/42"))
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }

    user, err := relay.DecodeJSON[APIUser](resp)
    if err != nil {
        t.Fatalf("decode failed: %v", err)
    }

    if user.Username != "alice" {
        t.Errorf("got username %q, want %q", user.Username, "alice")
    }
}
```

---

## Example 4 - Concurrent requests with bulkhead and context cancellation

When you need to fan out many requests in parallel, a bulkhead limits how many can be in-flight at once to protect the downstream service from being overwhelmed. Context cancellation lets you abort the whole batch cleanly.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "sync"
    "time"

    "github.com/jhonsferg/relay"
)

// Comment represents a single comment from the test API.
type Comment struct {
    PostID int    `json:"postId"`
    ID     int    `json:"id"`
    Name   string `json:"name"`
    Email  string `json:"email"`
    Body   string `json:"body"`
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://jsonplaceholder.typicode.com"),

        // WithBulkhead limits in-flight requests to 5 at a time.
        // Requests that exceed the limit wait for a slot; if the context
        // deadline passes while waiting they are cancelled with an error.
        relay.WithBulkhead(&relay.BulkheadConfig{
            MaxConcurrent: 5,
        }),

        relay.WithRetry(&relay.RetryConfig{
            MaxAttempts:     3,
            InitialInterval: 50 * time.Millisecond,
            Multiplier:      2.0,
        }),
    )

    // Fetch comments for posts 1 through 20 concurrently.
    postIDs := make([]int, 20)
    for i := range postIDs {
        postIDs[i] = i + 1
    }

    // Create a context with a 10-second deadline covering the whole batch.
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    type result struct {
        postID   int
        comments []Comment
        err      error
    }

    results := make(chan result, len(postIDs))
    var wg sync.WaitGroup

    for _, id := range postIDs {
        wg.Add(1)
        go func(postID int) {
            defer wg.Done()

            req := client.Get(fmt.Sprintf("/posts/%d/comments", postID))
            resp, err := client.Execute(ctx, req)
            if err != nil {
                results <- result{postID: postID, err: err}
                return
            }

            comments, err := relay.DecodeJSON[[]Comment](resp)
            results <- result{postID: postID, comments: comments, err: err}
        }(id)
    }

    // Close the results channel once all goroutines have finished.
    go func() {
        wg.Wait()
        close(results)
    }()

    // Collect and print a summary.
    totalComments := 0
    errCount := 0
    for r := range results {
        if r.err != nil {
            log.Printf("post %d failed: %v", r.postID, r.err)
            errCount++
            continue
        }
        totalComments += len(r.comments)
        fmt.Printf("Post %2d: %d comments\n", r.postID, len(r.comments))
    }

    fmt.Printf("\nFetched %d comments across %d posts (%d errors)\n",
        totalComments, len(postIDs)-errCount, errCount)
}
```

### What this demonstrates

- `relay.WithBulkhead` - cap in-flight concurrency to avoid overwhelming downstream
- `context.WithTimeout` - single deadline covering the entire concurrent batch
- Goroutine fan-out with a result channel and `sync.WaitGroup`
- Graceful error handling per goroutine without cancelling sibling requests
- The bulkhead transparently queues goroutines beyond the concurrency limit

**Expected output (order varies):**

```
Post  1: 5 comments
Post  2: 5 comments
Post  3: 5 comments
...
Post 20: 5 comments

Fetched 100 comments across 20 posts (0 errors)
```

### Cancelling the batch early

If any single goroutine encounters a critical error and you want to abort the rest, pass a cancellable context and call `cancel()`:

```go
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

for _, id := range postIDs {
    wg.Add(1)
    go func(postID int) {
        defer wg.Done()
        resp, err := client.Execute(ctx, client.Get(fmt.Sprintf("/posts/%d/comments", postID)))
        if err != nil {
            log.Printf("error on post %d: %v - cancelling batch", postID, err)
            cancel() // signal all other goroutines to stop
            results <- result{postID: postID, err: err}
            return
        }
        comments, _ := relay.DecodeJSON[[]Comment](resp)
        results <- result{postID: postID, comments: comments}
    }(id)
}
```

When `cancel()` is called, any goroutines waiting for a bulkhead slot or mid-request will receive a `context.Canceled` error and return immediately.

### Adding request hedging

Hedging races a duplicate request after a threshold to reduce tail latency. Enable it alongside the bulkhead:

```go
client := relay.New(
    relay.WithBaseURL("https://jsonplaceholder.typicode.com"),
    relay.WithBulkhead(&relay.BulkheadConfig{MaxConcurrent: 10}),
    // If no response arrives within 200ms, fire a second parallel request.
    // Whichever responds first wins; the other is cancelled.
    relay.WithHedging(&relay.HedgingConfig{
        Threshold: 200 * time.Millisecond,
        MaxHedged: 1,
    }),
)
```

> **Warning:** Hedging doubles (or more) the request load on your upstream when tail latency is high. Only enable it for idempotent read operations, and ensure your upstream can absorb the extra load. Never hedge non-idempotent writes.

---

## Next steps

- [Installation](installation.md) - extension modules, version pinning
- **Configuration reference** - every `WithXxx` option documented
- **Authentication** - Bearer, Basic, API Key, OAuth2, AWS SigV4
- **Resilience** - retry budgets, circuit breaker states, bulkhead tuning
- **Observability** - tracing, metrics, logging extensions
- **Testing** - mock transport, VCR cassettes, property-based testing helpers
