# Error Reference

relay uses standard Go error conventions: errors are values, and callers can inspect them using `errors.Is`, `errors.As`, and relay-specific predicate functions. This reference documents all error types, sentinel values, and helper functions exposed by the relay package.

---

## Error Handling Philosophy

relay distinguishes between three categories of failures:

1. **Network/transport errors** - connection refused, DNS failure, TLS handshake errors. These are often transient and may be safely retried.
2. **Timeout and cancellation errors** - the request exceeded its deadline or the caller cancelled the context. Retrying after context cancellation is never appropriate.
3. **HTTP-level errors** - the server responded with 4xx or 5xx. relay does not automatically convert these to Go errors; you must check `resp.IsError()` or inspect `resp.StatusCode`.

```go
resp, err := client.Execute(ctx, req)
if err != nil {
    // Transport/network/timeout/circuit error - no response received
    handleNetworkError(err)
    return
}
defer resp.Body.Close()

if resp.IsError() {
    // HTTP error - we got a response but it indicates failure
    handleHTTPError(resp)
    return
}

// Success path
```

---

## relay.IsRetryableError

```go
func IsRetryableError(err error) bool
```

`IsRetryableError` returns `true` when the error represents a transient condition that may succeed on retry. relay uses this function internally when evaluating whether to retry a request, and you can use it in custom retry predicates or error handling logic.

Errors considered retryable:
- Network connection errors (`net.Error` with temporary flag)
- HTTP 429 Too Many Requests (when returned as `*relay.HTTPError`)
- HTTP 500, 502, 503, 504 (when returned as `*relay.HTTPError`)
- `io.EOF` and `io.ErrUnexpectedEOF` during body read
- DNS resolution failures with temporary classification

Errors NOT considered retryable:
- `context.Canceled`
- `context.DeadlineExceeded`
- HTTP 400, 401, 403, 404, 422 (client errors - retrying won't help)
- Circuit breaker open (`relay.ErrCircuitOpen`)
- Bulkhead full (`relay.ErrBulkheadFull`) - the resource is persistently at capacity

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

func executeWithCustomRetry(client *relay.Client, req *relay.Request) (*relay.Response, error) {
    const maxAttempts = 5
    var lastErr error

    for attempt := 1; attempt <= maxAttempts; attempt++ {
        resp, err := client.Execute(context.Background(), req)
        if err == nil {
            return resp, nil
        }

        lastErr = err

        if !relay.IsRetryableError(err) {
            // Don't retry non-transient errors
            return nil, fmt.Errorf("non-retryable error on attempt %d: %w", attempt, err)
        }

        backoff := time.Duration(attempt*attempt) * 100 * time.Millisecond
        log.Printf("retryable error on attempt %d, backing off %s: %v", attempt, backoff, err)
        time.Sleep(backoff)
    }
    return nil, fmt.Errorf("exhausted %d attempts, last error: %w", maxAttempts, lastErr)
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))
    req := client.Get("/unstable-endpoint")

    resp, err := executeWithCustomRetry(client, req)
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("success:", resp.StatusCode)
}
```

---

## relay.IsTimeout

```go
func IsTimeout(err error) bool
```

`IsTimeout` returns `true` when the error was caused by a request timeout. This covers both:
- Per-request timeouts set via `req.WithTimeout` or `relay.WithTimeout`
- `context.DeadlineExceeded` from a context passed to `client.Execute`
- `net.Error` values with `Timeout() == true`

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
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTimeout(500*time.Millisecond), // very tight timeout for demo
    )

    resp, err := client.Execute(context.Background(), client.Get("/slow-endpoint"))
    if err != nil {
        if relay.IsTimeout(err) {
            log.Println("request timed out - consider increasing the timeout or using WithHedging")
            return
        }
        log.Fatal("unexpected error:", err)
    }
    defer resp.Body.Close()
    fmt.Println("got response:", resp.StatusCode)
}
```

---

## relay.IsCircuitOpen

```go
func IsCircuitOpen(err error) bool
```

`IsCircuitOpen` returns `true` when the request was rejected because the circuit breaker is in the open state. In this state, the upstream service is presumed to be unhealthy and requests are short-circuited to fail fast.

When the circuit is open, relay does not send any network request - it immediately returns this error. After the configured `Timeout` duration, the circuit enters a half-open state and allows one probe request through.

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
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
            MaxFailures: 3,
            Timeout:     30 * time.Second,
        }),
    )

    resp, err := client.Execute(context.Background(), client.Get("/data"))
    if err != nil {
        if relay.IsCircuitOpen(err) {
            // Serve from cache, return a degraded response, or shed load
            fmt.Println("circuit is open - upstream service unavailable")
            fmt.Println("serving cached data or returning 503")
            return
        }
        if relay.IsTimeout(err) {
            log.Println("request timed out")
            return
        }
        log.Fatal("error:", err)
    }
    defer resp.Body.Close()
    fmt.Println("response:", resp.StatusCode)
}
```

---

## relay.ErrBulkheadFull

```go
var ErrBulkheadFull error
```

`ErrBulkheadFull` is returned by `client.Execute` when the bulkhead concurrency limit (configured with `WithMaxConcurrentRequests`) is reached and the request's context is cancelled or times out while waiting for a slot to open.

This is a sentinel error and can be checked with `errors.Is`:

```go
if errors.Is(err, relay.ErrBulkheadFull) {
    // shed load or return 503
}
```

### Example

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log"
    "sync"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    // Only 2 concurrent requests allowed
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithMaxConcurrentRequests(2),
    )

    var wg sync.WaitGroup
    results := make([]string, 5)

    for i := range 5 {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()

            ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
            defer cancel()

            resp, err := client.Execute(ctx, client.Get("/work"))
            if err != nil {
                if errors.Is(err, relay.ErrBulkheadFull) {
                    results[i] = "shed (bulkhead full)"
                    return
                }
                results[i] = fmt.Sprintf("error: %v", err)
                return
            }
            defer resp.Body.Close()
            results[i] = fmt.Sprintf("ok: %d", resp.StatusCode)
        }(i)
    }

    wg.Wait()
    for i, r := range results {
        fmt.Printf("goroutine %d: %s\n", i, r)
    }
    _ = log.Writer()
}
```

---

## context.Canceled and context.DeadlineExceeded

relay propagates context errors transparently. When the context passed to `client.Execute` is cancelled or exceeds its deadline, the in-flight request is aborted and the context error is returned (possibly wrapped).

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

    // Simulate early cancellation
    ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
    defer cancel()

    resp, err := client.Execute(ctx, client.Get("/slow"))
    if err != nil {
        switch {
        case errors.Is(err, context.Canceled):
            fmt.Println("request was cancelled by caller")
        case errors.Is(err, context.DeadlineExceeded):
            fmt.Println("context deadline exceeded before response")
        case relay.IsTimeout(err):
            fmt.Println("relay-level timeout (per-request or client default)")
        default:
            log.Fatal("unexpected error:", err)
        }
        return
    }
    defer resp.Body.Close()
    fmt.Println("status:", resp.StatusCode)
}
```

---

## *relay.HTTPError

```go
type HTTPError struct {
    StatusCode int
    Status     string
    Body       []byte
}

func (e *HTTPError) Error() string
```

`*relay.HTTPError` is returned when you configure relay to automatically convert error HTTP responses into Go errors (for example, via a response hook or a custom middleware). It carries the status code, status text, and the raw response body for diagnostic purposes.

> **Note:** By default, relay does NOT return `*relay.HTTPError` from `client.Execute`. Non-2xx responses are returned as successful executions with a non-nil `*relay.Response`. You must check `resp.IsError()` manually, or configure relay to auto-error via a hook.

### Fields

| Field | Type | Description |
|-------|------|-------------|
| `StatusCode` | `int` | The HTTP status code (e.g., 404, 500). |
| `Status` | `string` | The full status text (e.g., `"404 Not Found"`). |
| `Body` | `[]byte` | The raw response body at the time of the error. |

### Checking with errors.As

```go
package main

import (
    "errors"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func handleError(err error) {
    var httpErr *relay.HTTPError
    if errors.As(err, &httpErr) {
        fmt.Printf("HTTP %d: %s\n", httpErr.StatusCode, httpErr.Status)
        fmt.Printf("body: %s\n", string(httpErr.Body))

        switch httpErr.StatusCode {
        case 400:
            fmt.Println("bad request - check your input")
        case 401:
            fmt.Println("unauthorized - check your credentials")
        case 403:
            fmt.Println("forbidden - insufficient permissions")
        case 404:
            fmt.Println("resource not found")
        case 429:
            fmt.Println("rate limited - back off and retry")
        case 500, 502, 503:
            fmt.Println("server error - may be transient")
        default:
            fmt.Printf("unhandled HTTP error: %d\n", httpErr.StatusCode)
        }
        return
    }
    log.Println("non-HTTP error:", err)
}
```

---

## Error Wrapping with fmt.Errorf and errors.Is/errors.As

relay wraps underlying errors using standard Go error wrapping (`fmt.Errorf("...: %w", err)`). You can always unwrap relay errors using the standard `errors.Is` and `errors.As` functions.

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log"

    "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))

    _, err := client.Execute(context.Background(), client.Get("/data"))
    if err != nil {
        // Unwrap through relay's error chain to find the root cause
        var httpErr *relay.HTTPError
        switch {
        case errors.As(err, &httpErr):
            fmt.Printf("HTTP %d: %s\n", httpErr.StatusCode, httpErr.Status)
        case errors.Is(err, context.Canceled):
            fmt.Println("cancelled")
        case errors.Is(err, context.DeadlineExceeded):
            fmt.Println("deadline exceeded")
        case relay.IsCircuitOpen(err):
            fmt.Println("circuit breaker is open")
        case errors.Is(err, relay.ErrBulkheadFull):
            fmt.Println("bulkhead at capacity")
        case relay.IsTimeout(err):
            fmt.Println("timed out")
        case relay.IsRetryableError(err):
            fmt.Println("retryable network error:", err)
        default:
            log.Println("unclassified error:", err)
        }
    }
}
```

---

## Complete Switch-Case Error Handling Pattern

The following is the recommended complete error handling pattern covering all error types returned by relay. Use this as a template for production error handling middleware or service-level error boundaries:

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log"
    "net/http"
    "time"

    "github.com/jhonsferg/relay"
)

type ServiceError struct {
    Code    int
    Message string
    Retry   bool
}

func (e *ServiceError) Error() string {
    return fmt.Sprintf("service error %d: %s", e.Code, e.Message)
}

// executeWithFullErrorHandling executes a request and translates all relay
// errors into application-level ServiceErrors with appropriate retry hints.
func executeWithFullErrorHandling(
    client *relay.Client,
    ctx context.Context,
    req *relay.Request,
) (*relay.Response, *ServiceError) {
    resp, err := client.Execute(ctx, req)

    if err != nil {
        var httpErr *relay.HTTPError

        switch {
        case errors.Is(err, context.Canceled):
            return nil, &ServiceError{
                Code:    499, // nginx-style "client closed request"
                Message: "request was cancelled",
                Retry:   false,
            }

        case errors.Is(err, context.DeadlineExceeded):
            return nil, &ServiceError{
                Code:    504,
                Message: "upstream deadline exceeded",
                Retry:   true,
            }

        case relay.IsTimeout(err):
            return nil, &ServiceError{
                Code:    504,
                Message: "upstream request timed out",
                Retry:   true,
            }

        case relay.IsCircuitOpen(err):
            return nil, &ServiceError{
                Code:    503,
                Message: "upstream circuit breaker open - service unavailable",
                Retry:   false, // don't retry immediately; wait for circuit to half-open
            }

        case errors.Is(err, relay.ErrBulkheadFull):
            return nil, &ServiceError{
                Code:    503,
                Message: "concurrency limit reached - shed load",
                Retry:   true,
            }

        case errors.As(err, &httpErr):
            return nil, &ServiceError{
                Code:    httpErr.StatusCode,
                Message: httpErr.Status,
                Retry:   relay.IsRetryableError(err),
            }

        case relay.IsRetryableError(err):
            return nil, &ServiceError{
                Code:    502,
                Message: fmt.Sprintf("transient upstream error: %v", err),
                Retry:   true,
            }

        default:
            return nil, &ServiceError{
                Code:    500,
                Message: fmt.Sprintf("unexpected error: %v", err),
                Retry:   false,
            }
        }
    }

    return resp, nil
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTimeout(10*time.Second),
        relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
            MaxFailures: 5,
            Timeout:     30 * time.Second,
        }),
        relay.WithMaxConcurrentRequests(20),
    )

    ctx := context.Background()
    resp, svcErr := executeWithFullErrorHandling(client, ctx, client.Get("/data"))
    if svcErr != nil {
        if svcErr.Retry {
            log.Printf("retryable error (code %d): %s", svcErr.Code, svcErr.Message)
        } else {
            log.Printf("fatal error (code %d): %s", svcErr.Code, svcErr.Message)
        }
        return
    }
    defer resp.Body.Close()

    if !resp.IsSuccess() {
        log.Printf("HTTP error: %d", resp.StatusCode)
        return
    }

    body, err := resp.Text()
    if err != nil {
        log.Fatal("read error:", err)
    }
    fmt.Println("response:", body)

    _ = http.StatusOK
}
```
