# Testing with relay

relay is designed to be fully testable without network access. The `mock` transport and the `recorder` let you write deterministic, fast tests for any code that uses a relay client.

## Mock transport

`relay.NewMockTransport()` returns an `http.RoundTripper` that serves pre-configured responses from a queue. No listening server required.

### Single response

```go
import (
    "testing"
    "github.com/jhonsferg/relay"
)

func TestGetUser(t *testing.T) {
    mt := relay.NewMockTransport()
    mt.Enqueue(&http.Response{
        StatusCode: 200,
        Body: io.NopCloser(strings.NewReader(`{"id":1,"name":"Alice"}`)),
        Header: http.Header{"Content-Type": []string{"application/json"}},
    })

    client := relay.New(relay.Config{Transport: mt})

    var user struct {
        ID   int    `json:"id"`
        Name string `json:"name"`
    }
    _, err := client.R().SetResult(&user).GET(context.Background(), "/users/1")
    if err != nil {
        t.Fatal(err)
    }
    if user.Name != "Alice" {
        t.Errorf("got %q, want Alice", user.Name)
    }
}
```

### Multiple sequential responses

```go
mt := relay.NewMockTransport()

// First call returns 503
mt.Enqueue(&http.Response{StatusCode: 503, Body: http.NoBody})
// retry 1 - still 503
mt.Enqueue(&http.Response{StatusCode: 503, Body: http.NoBody})
// retry 2 - still 503
mt.Enqueue(&http.Response{StatusCode: 503, Body: http.NoBody})

client := relay.New(relay.Config{
    Transport:       mt,
    MaxRetries:      3,
    RetryableStatus: []int{503},
})

_, err := client.R().GET(context.Background(), "/flaky")
// err is non-nil after exhausting retries
```

!!! note "Retry queuing"
    relay retries include the original attempt. `MaxRetries: 3` means up to 3 total requests (1 original + 2 retries - depending on config). Enqueue enough responses for all attempts.

### Asserting request details

```go
mt := relay.NewMockTransport()
mt.Enqueue(&http.Response{
    StatusCode: 200,
    Body:       io.NopCloser(strings.NewReader(`{}`)),
})

client := relay.New(relay.Config{Transport: mt})
client.R().
    SetHeader("X-Api-Key", "secret").
    GET(context.Background(), "/secure")

req := mt.LastRequest()
if req.Header.Get("X-Api-Key") != "secret" {
    t.Error("expected API key header")
}
if mt.CallCount() != 1 {
    t.Errorf("expected 1 call, got %d", mt.CallCount())
}
```

## HTTPtest server

For integration-style tests that need real HTTP semantics, use Go's `net/http/httptest`:

```go
func TestWithRealServer(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.URL.Path != "/users/42" {
            http.NotFound(w, r)
            return
        }
        w.Header().Set("Content-Type", "application/json")
        w.Write([]byte(`{"id":42,"name":"Bob"}`))
    }))
    defer srv.Close()

    client := relay.New(relay.Config{BaseURL: srv.URL})

    var user struct {
        ID   int    `json:"id"`
        Name string `json:"name"`
    }
    _, err := client.R().SetResult(&user).GET(context.Background(), "/users/42")
    if err != nil {
        t.Fatal(err)
    }
    if user.ID != 42 {
        t.Errorf("got id %d, want 42", user.ID)
    }
}
```

## Request recorder

The recorder captures all outgoing requests for later assertions without queuing responses. Combine it with a mock transport:

```go
rec := relay.NewRecorder()
mt := relay.NewMockTransport()
mt.Enqueue(okResponse(`{"status":"ok"}`))

client := relay.New(relay.Config{
    Transport: relay.ChainTransport(rec, mt),
})

client.R().
    SetHeader("Authorization", "Bearer token123").
    POST(context.Background(), "/v1/events", map[string]string{"type": "click"})

calls := rec.Calls()
if len(calls) != 1 {
    t.Fatalf("expected 1 call, got %d", len(calls))
}

body, _ := io.ReadAll(calls[0].Request.Body)
if !strings.Contains(string(body), "click") {
    t.Error("expected body to contain 'click'")
}
```

## Table-driven tests

```go
func TestStatusHandling(t *testing.T) {
    cases := []struct {
        name       string
        statusCode int
        wantErr    bool
    }{
        {"ok", 200, false},
        {"not found", 404, true},
        {"server error", 500, true},
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            mt := relay.NewMockTransport()
            mt.Enqueue(&http.Response{
                StatusCode: tc.statusCode,
                Body:       http.NoBody,
            })

            client := relay.New(relay.Config{
                Transport:  mt,
                MaxRetries: 0, // disable retries for determinism
            })

            _, err := client.R().GET(context.Background(), "/check")
            if (err != nil) != tc.wantErr {
                t.Errorf("wantErr=%v got err=%v", tc.wantErr, err)
            }
        })
    }
}
```

## Testing hooks

```go
func TestOnRetryHook(t *testing.T) {
    var attempts int

    mt := relay.NewMockTransport()
    mt.Enqueue(&http.Response{StatusCode: 500, Body: http.NoBody})
    mt.Enqueue(&http.Response{StatusCode: 500, Body: http.NoBody})
    mt.Enqueue(&http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))})

    client := relay.New(relay.Config{
        Transport:       mt,
        MaxRetries:      3,
        RetryableStatus: []int{500},
        OnRetry: func(n int, _ *http.Request, _ *http.Response, _ error) {
            attempts = n
        },
    })

    client.R().GET(context.Background(), "/flaky")

    if attempts != 2 {
        t.Errorf("expected 2 retry callbacks, got %d", attempts)
    }
}
```

## Helper functions

```go
// helpers_test.go
func okResponse(body string) *http.Response {
    return &http.Response{
        StatusCode: 200,
        Body:       io.NopCloser(strings.NewReader(body)),
        Header:     http.Header{"Content-Type": []string{"application/json"}},
    }
}

func errorResponse(code int) *http.Response {
    return &http.Response{
        StatusCode: code,
        Body:       http.NoBody,
    }
}
```

## See also

- [Mock Transport extension](../extensions/mock.md) - advanced mock patterns
- [Error Handling](error-handling.md)
- [Hooks & Middleware](hooks.md)
