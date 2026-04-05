# Mock Transport Extension

The Mock Transport extension provides a test-friendly HTTP transport that records requests and serves pre-configured responses. By injecting it into a relay client, you can write fast, deterministic unit tests that exercise your full request pipeline - middleware, serialization, error handling - without any network calls.

**Import path:** `github.com/jhonsferg/relay/ext/mock`

---

## Overview

Testing HTTP client code typically requires either a real server (slow, fragile) or a `httptest.Server` (better, but still starts a real TCP listener). The mock transport operates entirely in memory: it never opens a socket, never allocates an OS port, and runs without any goroutine overhead beyond the test itself.

Key capabilities:
- Queue static responses with `Enqueue`
- Queue dynamic responses with `EnqueueFunc`
- Record every request for assertion
- Simulate network errors and timeouts
- Thread-safe: safe to use from parallel sub-tests
- Zero dependencies beyond the Go standard library

---

## Installation

```bash
go get github.com/jhonsferg/relay/ext/mock@latest
```

---

## API Reference

### `relaymock.NewTransport`

```go
func NewTransport() *MockTransport
```

Creates a new `MockTransport` with an empty queue and an empty request log.

### `MockTransport.Enqueue`

```go
func (t *MockTransport) Enqueue(resp *http.Response)
```

Adds a static `*http.Response` to the response queue. Requests are served in FIFO order. If the queue is empty when a request arrives, the transport returns an error.

### `MockTransport.EnqueueFunc`

```go
func (t *MockTransport) EnqueueFunc(fn func(req *http.Request) (*http.Response, error))
```

Adds a dynamic response function to the queue. The function receives the incoming `*http.Request` and may return any response or error. Use this to inspect request parameters, simulate errors conditionally, or return different responses based on request content.

### `MockTransport.RecordedRequests`

```go
func (t *MockTransport) RecordedRequests() []*http.Request
```

Returns a copy of all requests the transport has handled, in order. Requests are recorded before the queued response or function is invoked, so the slice is accurate even if a queued function panics.

### `MockTransport.Reset`

```go
func (t *MockTransport) Reset()
```

Clears both the response queue and the recorded request log. Useful between sub-tests that share a transport instance.

### `MockTransport.QueueLength`

```go
func (t *MockTransport) QueueLength() int
```

Returns the number of responses still waiting in the queue. Use this in tests to assert that all expected requests were made.

---

## Basic Usage

```go
package main_test

import (
    "context"
    "io"
    "net/http"
    "strings"
    "testing"

    relay "github.com/jhonsferg/relay"
    relaymock "github.com/jhonsferg/relay/ext/mock"
)

func TestGetUser(t *testing.T) {
    transport := relaymock.NewTransport()

    // Queue a response that the client will receive.
    transport.EnqueueFunc(func(req *http.Request) (*http.Response, error) {
        return &http.Response{
            StatusCode: http.StatusOK,
            Header: http.Header{
                "Content-Type": []string{"application/json"},
            },
            Body: io.NopCloser(strings.NewReader(`{
                "id": 1,
                "login": "octocat",
                "name": "The Octocat",
                "company": "GitHub"
            }`)),
        }, nil
    })

    client, err := relay.NewClient(
        relay.WithBaseURL("https://api.github.com"),
        relay.WithTransport(transport),
    )
    if err != nil {
        t.Fatalf("create client: %v", err)
    }

    type User struct {
        ID      int    `json:"id"`
        Login   string `json:"login"`
        Name    string `json:"name"`
        Company string `json:"company"`
    }

    var user User
    if err := client.Get(context.Background(), "/users/octocat", &user); err != nil {
        t.Fatalf("get user: %v", err)
    }

    if user.Login != "octocat" {
        t.Errorf("expected login=octocat, got %q", user.Login)
    }
    if user.Name != "The Octocat" {
        t.Errorf("expected name='The Octocat', got %q", user.Name)
    }

    // Verify the request was recorded.
    reqs := transport.RecordedRequests()
    if len(reqs) != 1 {
        t.Fatalf("expected 1 recorded request, got %d", len(reqs))
    }
}
```

---

## Asserting Request Properties

The mock transport records the full `*http.Request`, giving you access to the URL, method, headers, and body:

```go
func TestCreateIssue(t *testing.T) {
    transport := relaymock.NewTransport()

    transport.EnqueueFunc(func(req *http.Request) (*http.Response, error) {
        return &http.Response{
            StatusCode: http.StatusCreated,
            Header:     http.Header{"Content-Type": []string{"application/json"}},
            Body: io.NopCloser(strings.NewReader(`{
                "id": 42,
                "number": 1,
                "title": "Bug: nil pointer in handler",
                "state": "open"
            }`)),
        }, nil
    })

    client, _ := relay.NewClient(
        relay.WithBaseURL("https://api.github.com"),
        relay.WithHeader("Authorization", "Bearer test-token"),
        relay.WithHeader("X-Request-ID", "req-123"),
        relay.WithTransport(transport),
    )

    type CreateIssueRequest struct {
        Title  string   `json:"title"`
        Body   string   `json:"body"`
        Labels []string `json:"labels"`
    }
    type Issue struct {
        ID     int    `json:"id"`
        Number int    `json:"number"`
        Title  string `json:"title"`
        State  string `json:"state"`
    }

    var issue Issue
    err := client.Post(context.Background(), "/repos/myorg/myrepo/issues",
        &CreateIssueRequest{
            Title:  "Bug: nil pointer in handler",
            Body:   "Steps to reproduce...",
            Labels: []string{"bug", "priority:high"},
        }, &issue)
    if err != nil {
        t.Fatalf("post issue: %v", err)
    }

    // --- Assert on recorded request ---
    reqs := transport.RecordedRequests()
    if len(reqs) != 1 {
        t.Fatalf("expected 1 request, got %d", len(reqs))
    }
    req := reqs[0]

    // Method
    if req.Method != http.MethodPost {
        t.Errorf("expected POST, got %s", req.Method)
    }

    // URL path
    if req.URL.Path != "/repos/myorg/myrepo/issues" {
        t.Errorf("unexpected path: %s", req.URL.Path)
    }

    // Authorization header
    if got := req.Header.Get("Authorization"); got != "Bearer test-token" {
        t.Errorf("expected Authorization 'Bearer test-token', got %q", got)
    }

    // Custom header propagation
    if got := req.Header.Get("X-Request-ID"); got != "req-123" {
        t.Errorf("expected X-Request-ID 'req-123', got %q", got)
    }

    // Request body
    bodyBytes, _ := io.ReadAll(req.Body)
    bodyStr := string(bodyBytes)
    if !strings.Contains(bodyStr, "nil pointer in handler") {
        t.Errorf("request body does not contain issue title: %s", bodyStr)
    }
    if !strings.Contains(bodyStr, `"priority:high"`) {
        t.Errorf("request body does not contain label: %s", bodyStr)
    }

    // Response
    if issue.Number != 1 {
        t.Errorf("expected issue number 1, got %d", issue.Number)
    }
}
```

---

## Simulating Errors

```go
func TestNetworkError(t *testing.T) {
    transport := relaymock.NewTransport()

    // Simulate a connection refused error.
    transport.EnqueueFunc(func(req *http.Request) (*http.Response, error) {
        return nil, &net.OpError{
            Op:  "dial",
            Net: "tcp",
            Err: &net.AddrError{Err: "connection refused", Addr: "api.github.com:443"},
        }
    })

    client, _ := relay.NewClient(
        relay.WithBaseURL("https://api.github.com"),
        relay.WithTransport(transport),
        // Disable retries so the error surfaces immediately.
        relay.WithRetry(relay.RetryConfig{MaxAttempts: 1}),
    )

    var result map[string]any
    err := client.Get(context.Background(), "/users/octocat", &result)
    if err == nil {
        t.Fatal("expected error, got nil")
    }
    t.Logf("got expected error: %v", err)
}

func TestServerError(t *testing.T) {
    transport := relaymock.NewTransport()

    transport.EnqueueFunc(func(req *http.Request) (*http.Response, error) {
        return &http.Response{
            StatusCode: http.StatusInternalServerError,
            Header:     http.Header{"Content-Type": []string{"application/json"}},
            Body: io.NopCloser(strings.NewReader(`{
                "message": "Internal Server Error",
                "code": "INTERNAL_ERROR"
            }`)),
        }, nil
    })

    client, _ := relay.NewClient(
        relay.WithBaseURL("https://api.github.com"),
        relay.WithTransport(transport),
    )

    var result map[string]any
    err := client.Get(context.Background(), "/users/octocat", &result)
    if err == nil {
        t.Fatal("expected error for 500 response")
    }

    var httpErr *relay.HTTPError
    if !errors.As(err, &httpErr) {
        t.Fatalf("expected *relay.HTTPError, got %T: %v", err, err)
    }
    if httpErr.StatusCode != http.StatusInternalServerError {
        t.Errorf("expected status 500, got %d", httpErr.StatusCode)
    }
}
```

---

## Testing Retry Logic

Queue multiple responses to test retry behavior:

```go
func TestRetryOn503(t *testing.T) {
    transport := relaymock.NewTransport()

    callCount := 0

    // First two calls return 503, third returns 200.
    for i := 0; i < 2; i++ {
        transport.EnqueueFunc(func(req *http.Request) (*http.Response, error) {
            callCount++
            return &http.Response{
                StatusCode: http.StatusServiceUnavailable,
                Header:     http.Header{"Content-Type": []string{"application/json"}},
                Body:       io.NopCloser(strings.NewReader(`{"error":"service unavailable"}`)),
            }, nil
        })
    }
    transport.EnqueueFunc(func(req *http.Request) (*http.Response, error) {
        callCount++
        return &http.Response{
            StatusCode: http.StatusOK,
            Header:     http.Header{"Content-Type": []string{"application/json"}},
            Body:       io.NopCloser(strings.NewReader(`{"status":"ok"}`)),
        }, nil
    })

    client, _ := relay.NewClient(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTransport(transport),
        relay.WithRetry(relay.RetryConfig{
            MaxAttempts:     3,
            WaitBase:        0, // no delay in tests
            RetryableStatus: []int{503},
        }),
    )

    var result map[string]any
    if err := client.Get(context.Background(), "/health", &result); err != nil {
        t.Fatalf("expected success after retries, got: %v", err)
    }
    if callCount != 3 {
        t.Errorf("expected 3 calls (2 failures + 1 success), got %d", callCount)
    }
    if transport.QueueLength() != 0 {
        t.Errorf("expected empty queue after test, got %d items", transport.QueueLength())
    }
}
```

---

## Complete Unit Test Example

This example shows a complete test file for a hypothetical GitHub client package:

```go
package githubclient_test

import (
    "context"
    "encoding/json"
    "errors"
    "io"
    "net"
    "net/http"
    "strings"
    "testing"

    relay "github.com/jhonsferg/relay"
    relaymock "github.com/jhonsferg/relay/ext/mock"
)

// GHClient wraps relay.Client with GitHub-specific methods.
type GHClient struct {
    relay *relay.Client
}

func NewGHClient(rc *relay.Client) *GHClient {
    return &GHClient{relay: rc}
}

type GHUser struct {
    Login     string `json:"login"`
    Name      string `json:"name"`
    Followers int    `json:"followers"`
}

func (c *GHClient) GetUser(ctx context.Context, username string) (*GHUser, error) {
    var user GHUser
    if err := c.relay.Get(ctx, "/users/"+username, &user); err != nil {
        return nil, err
    }
    return &user, nil
}

func (c *GHClient) ListRepos(ctx context.Context, username string) ([]map[string]any, error) {
    var repos []map[string]any
    if err := c.relay.Get(ctx, "/users/"+username+"/repos", &repos); err != nil {
        return nil, err
    }
    return repos, nil
}

// --- Tests ---

func newTestClient(t *testing.T, transport *relaymock.MockTransport) *GHClient {
    t.Helper()
    rc, err := relay.NewClient(
        relay.WithBaseURL("https://api.github.com"),
        relay.WithHeader("Authorization", "Bearer test-token"),
        relay.WithTransport(transport),
    )
    if err != nil {
        t.Fatalf("create relay client: %v", err)
    }
    return NewGHClient(rc)
}

func enqueueJSON(t *testing.T, transport *relaymock.MockTransport, status int, body any) {
    t.Helper()
    data, err := json.Marshal(body)
    if err != nil {
        t.Fatalf("marshal response body: %v", err)
    }
    transport.EnqueueFunc(func(req *http.Request) (*http.Response, error) {
        return &http.Response{
            StatusCode: status,
            Header:     http.Header{"Content-Type": []string{"application/json"}},
            Body:       io.NopCloser(strings.NewReader(string(data))),
        }, nil
    })
}

func TestGetUser_Success(t *testing.T) {
    transport := relaymock.NewTransport()
    enqueueJSON(t, transport, http.StatusOK, map[string]any{
        "login":     "octocat",
        "name":      "The Octocat",
        "followers": 9999,
    })

    client := newTestClient(t, transport)
    user, err := client.GetUser(context.Background(), "octocat")
    if err != nil {
        t.Fatalf("GetUser: %v", err)
    }

    if user.Login != "octocat" {
        t.Errorf("login: got %q, want %q", user.Login, "octocat")
    }
    if user.Followers != 9999 {
        t.Errorf("followers: got %d, want 9999", user.Followers)
    }

    // Assert the request URL contains the username.
    reqs := transport.RecordedRequests()
    if len(reqs) != 1 {
        t.Fatalf("expected 1 request, got %d", len(reqs))
    }
    if !strings.HasSuffix(reqs[0].URL.Path, "/octocat") {
        t.Errorf("unexpected path: %s", reqs[0].URL.Path)
    }
}

func TestGetUser_NotFound(t *testing.T) {
    transport := relaymock.NewTransport()
    enqueueJSON(t, transport, http.StatusNotFound, map[string]any{
        "message":           "Not Found",
        "documentation_url": "https://docs.github.com/rest",
    })

    client := newTestClient(t, transport)
    _, err := client.GetUser(context.Background(), "doesnotexist99999")
    if err == nil {
        t.Fatal("expected error for 404 response")
    }

    var httpErr *relay.HTTPError
    if !errors.As(err, &httpErr) {
        t.Fatalf("expected *relay.HTTPError, got %T", err)
    }
    if httpErr.StatusCode != http.StatusNotFound {
        t.Errorf("expected 404, got %d", httpErr.StatusCode)
    }
}

func TestGetUser_NetworkError(t *testing.T) {
    transport := relaymock.NewTransport()
    transport.EnqueueFunc(func(req *http.Request) (*http.Response, error) {
        return nil, &net.OpError{
            Op:  "dial",
            Net: "tcp",
            Err: errors.New("connection refused"),
        }
    })

    client := newTestClient(t, transport)
    _, err := client.GetUser(context.Background(), "octocat")
    if err == nil {
        t.Fatal("expected network error")
    }

    var netErr *net.OpError
    if !errors.As(err, &netErr) {
        t.Errorf("expected *net.OpError, got %T: %v", err, err)
    }
}

func TestListRepos_Empty(t *testing.T) {
    transport := relaymock.NewTransport()
    enqueueJSON(t, transport, http.StatusOK, []any{})

    client := newTestClient(t, transport)
    repos, err := client.ListRepos(context.Background(), "octocat")
    if err != nil {
        t.Fatalf("ListRepos: %v", err)
    }
    if len(repos) != 0 {
        t.Errorf("expected 0 repos, got %d", len(repos))
    }
}

func TestQueueExhausted(t *testing.T) {
    transport := relaymock.NewTransport()
    // No responses queued - second call should error.
    enqueueJSON(t, transport, http.StatusOK, map[string]any{"login": "octocat", "name": "The Octocat"})

    client := newTestClient(t, transport)

    // First call succeeds.
    if _, err := client.GetUser(context.Background(), "octocat"); err != nil {
        t.Fatalf("first call: %v", err)
    }

    // Second call fails because queue is empty.
    _, err := client.GetUser(context.Background(), "octocat")
    if err == nil {
        t.Fatal("expected error when queue is exhausted")
    }
    if !strings.Contains(err.Error(), "mock transport: queue is empty") {
        t.Errorf("unexpected error message: %v", err)
    }
}
```

---

## Parallel Sub-Test Safety

`MockTransport` is safe to use from concurrent goroutines. However, when using parallel sub-tests, each sub-test should use its own transport instance to avoid cross-test interference:

```go
func TestParallel(t *testing.T) {
    users := []string{"octocat", "torvalds", "gvanrossum"}

    for _, username := range users {
        username := username // capture loop variable
        t.Run(username, func(t *testing.T) {
            t.Parallel()

            // Each parallel sub-test gets its own transport.
            transport := relaymock.NewTransport()
            enqueueJSON(t, transport, http.StatusOK, map[string]any{
                "login": username,
                "name":  strings.Title(username),
            })

            client := newTestClient(t, transport)
            user, err := client.GetUser(context.Background(), username)
            if err != nil {
                t.Fatalf("GetUser(%s): %v", username, err)
            }
            if user.Login != username {
                t.Errorf("expected login=%s, got %s", username, user.Login)
            }
        })
    }
}
```

---

## See Also

- [gRPC Bridge Extension](grpc.md) - testing gRPC calls with mock transport
- [GraphQL Extension](graphql.md) - testing GraphQL queries with mock transport
- relay core documentation - using `relay.WithTransport` for custom transports
