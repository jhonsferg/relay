# Error Handling

relay uses Go's standard `error` interface throughout. Errors fall into three
broad categories:

1. **Sentinel errors** - compare with `errors.Is` (e.g. `relay.ErrBulkheadFull`).
2. **Typed errors** - use `errors.As` or the helper `relay.IsHTTPError` to
   extract structured data (e.g. `*relay.HTTPError`).
3. **Classification helpers** - `relay.ClassifyError`, `relay.IsRetryableError`,
   `relay.IsTimeout`, `relay.IsCircuitOpen` let you categorise any error
   without inspecting its type directly.

---

## Sentinel errors

| Sentinel | Meaning |
|----------|---------|
| `relay.ErrCircuitOpen` | Circuit breaker is open; request was rejected without being sent |
| `relay.ErrMaxRetriesReached` | All retry attempts exhausted |
| `relay.ErrRateLimitExceeded` | Rate limiter could not grant a token before context expired |
| `relay.ErrNilRequest` | A `nil` `*relay.Request` was passed to `Execute` |
| `relay.ErrTimeout` | Per-request timeout fired |
| `relay.ErrBodyTruncated` | Response body exceeded the configured size limit |
| `relay.ErrClientClosed` | `Execute` called after `Shutdown` |
| `relay.ErrBulkheadFull` | Bulkhead slot limit reached and context cancelled |

Always use `errors.Is` rather than `==` for sentinel comparisons, because relay
may wrap these errors with additional context:

```go
package main

import (
    "context"
    "errors"
    "fmt"

    relay "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))
    defer client.Shutdown(context.Background()) //nolint:errcheck

    _, err := client.Execute(client.Get("/data"))
    if err == nil {
        return
    }

    switch {
    case errors.Is(err, relay.ErrCircuitOpen):
        fmt.Println("circuit breaker is open, back off and retry later")
    case errors.Is(err, relay.ErrMaxRetriesReached):
        fmt.Println("all retries exhausted")
    case errors.Is(err, relay.ErrBulkheadFull):
        fmt.Println("too many concurrent requests, shed load")
    case errors.Is(err, relay.ErrTimeout):
        fmt.Println("request timed out")
    case errors.Is(err, relay.ErrRateLimitExceeded):
        fmt.Println("local rate limit exceeded")
    default:
        fmt.Printf("unexpected error: %v\n", err)
    }
}
```

---

## ErrBulkheadFull

`ErrBulkheadFull` is returned when the bulkhead (concurrency limiter) has no
available slots and the context is cancelled or times out before one becomes
free. It signals that the application is under too much load to accept more
work right now.

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "net/http"
    "time"

    relay "github.com/jhonsferg/relay"
)

func callWithBulkhead(client *relay.Client, path string) error {
    ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
    defer cancel()

    _, err := client.Execute(client.Get(path).WithContext(ctx))
    if errors.Is(err, relay.ErrBulkheadFull) {
        // Return a 503 to the caller or enqueue for later.
        return fmt.Errorf("service unavailable: %w", err)
    }
    return err
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithBulkhead(5), // max 5 concurrent in-flight requests
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    if err := callWithBulkhead(client, "/orders"); err != nil {
        fmt.Println(err)
    }
    _ = http.StatusServiceUnavailable // referenced above conceptually
}
```

> **Note**
> `ErrBulkheadFull` is distinct from `ErrRateLimitExceeded`. The rate limiter
> throttles the *rate* of requests (requests per second), while the bulkhead
> limits *concurrency* (requests in-flight simultaneously).

---

## Classification helpers

### IsRetryableError

`IsRetryableError(err, resp)` returns `true` when the error or response
indicates that retrying is worthwhile - that is, the error class is
`ErrorClassTransient` or `ErrorClassRateLimited`.

```go
package main

import (
    "context"
    "fmt"
    "time"

    relay "github.com/jhonsferg/relay"
)

func fetchWithBackoff(client *relay.Client, path string) error {
    var (
        resp *relay.Response
        err  error
    )

    for attempt := 1; attempt <= 3; attempt++ {
        resp, err = client.Execute(client.Get(path))
        if !relay.IsRetryableError(err, resp) {
            break
        }
        fmt.Printf("attempt %d failed (retryable): %v\n", attempt, err)
        time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
    }
    return err
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))
    defer client.Shutdown(context.Background()) //nolint:errcheck

    if err := fetchWithBackoff(client, "/unstable"); err != nil {
        fmt.Println("final error:", err)
    }
}
```

### IsTimeout

`IsTimeout(err)` returns `true` for both `relay.ErrTimeout` (per-request
timeout) and `context.DeadlineExceeded` (caller-supplied deadline):

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "time"

    relay "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTimeout(2*time.Second),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    _, err := client.Execute(client.Get("/slow-endpoint"))

    if relay.IsTimeout(err) {
        fmt.Println("timed out - check server performance")
        return
    }
    if err != nil {
        fmt.Println("other error:", err)
    }
}
```

> **Tip**
> `IsTimeout` matches both the relay-level timeout (`WithTimeout`) and any
> deadline inherited from a parent `context.Context`. You do not need to check
> `errors.Is(err, context.DeadlineExceeded)` separately.

### IsCircuitOpen

`IsCircuitOpen(err)` returns `true` when `errors.Is(err, relay.ErrCircuitOpen)`:

```go
package main

import (
    "context"
    "fmt"
    "time"

    relay "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithCircuitBreaker(relay.CircuitBreakerConfig{
            Threshold:   5,
            OpenTimeout: 10 * time.Second,
        }),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    _, err := client.Execute(client.Get("/payments"))
    if relay.IsCircuitOpen(err) {
        fmt.Println("circuit is open - returning cached response or failing fast")
    }
}
```

---

## Context cancellation

relay propagates context errors faithfully. Use `errors.Is` to distinguish
between a caller-initiated cancellation and a deadline expiry:

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "time"

    relay "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))
    defer client.Shutdown(context.Background()) //nolint:errcheck

    // Simulate a context cancelled by the caller (e.g. HTTP handler returning early).
    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
    defer cancel()

    _, err := client.Execute(client.Get("/slow").WithContext(ctx))

    switch {
    case err == nil:
        fmt.Println("success")
    case errors.Is(err, context.Canceled):
        fmt.Println("caller cancelled the request")
    case errors.Is(err, context.DeadlineExceeded):
        fmt.Println("deadline exceeded")
    case relay.IsTimeout(err):
        // Relay-level per-request timeout (WithTimeout).
        fmt.Println("relay timeout")
    default:
        fmt.Println("other error:", err)
    }
}
```

---

## HTTP error status codes vs Go errors

relay does **not** automatically convert non-2xx responses into Go errors.
A `*relay.Response` with `StatusCode == 503` is returned alongside a `nil`
error. Inspect the response yourself, or call `resp.AsHTTPError()` to get a
typed error:

```go
package main

import (
    "context"
    "errors"
    "fmt"

    relay "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))
    defer client.Shutdown(context.Background()) //nolint:errcheck

    resp, err := client.Execute(client.Get("/users/1"))
    if err != nil {
        fmt.Println("transport error:", err)
        return
    }

    // Convert a non-2xx response into a Go error.
    if httpErr := resp.AsHTTPError(); httpErr != nil {
        fmt.Printf("HTTP %d: %s\n", httpErr.StatusCode, httpErr.Body)
        return
    }

    fmt.Println("body:", resp.String())
}
```

Use `relay.IsHTTPError` when the error may already be wrapped in an error chain:

```go
package main

import (
    "context"
    "fmt"

    relay "github.com/jhonsferg/relay"
)

func handleErr(err error) {
    if httpErr, ok := relay.IsHTTPError(err); ok {
        switch {
        case httpErr.StatusCode == 401:
            fmt.Println("unauthorised - refresh credentials")
        case httpErr.StatusCode == 404:
            fmt.Println("resource not found")
        case httpErr.StatusCode >= 500:
            fmt.Println("server error - retry later")
        }
        return
    }
    fmt.Println("non-HTTP error:", err)
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))
    defer client.Shutdown(context.Background()) //nolint:errcheck

    resp, err := client.Execute(client.Get("/resource"))
    if err == nil && resp != nil {
        err = resp.AsHTTPError()
    }
    if err != nil {
        handleErr(err)
    }
}
```

---

## Switch-case pattern for comprehensive error handling

The following switch-case pattern covers every error class relay can produce
and provides a template you can copy into production code:

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log/slog"
    "time"

    relay "github.com/jhonsferg/relay"
)

// handleRelayError translates a relay error into an application-level decision.
// Returns true if the caller should retry at the application layer.
func handleRelayError(ctx context.Context, err error, resp *relay.Response) (retry bool) {
    if err == nil {
        return false
    }

    switch {
    case errors.Is(err, relay.ErrCircuitOpen):
        slog.WarnContext(ctx, "circuit open - fast fail")
        return false

    case errors.Is(err, relay.ErrBulkheadFull):
        slog.WarnContext(ctx, "bulkhead full - shed load")
        return false

    case errors.Is(err, relay.ErrMaxRetriesReached):
        slog.ErrorContext(ctx, "max retries exhausted")
        return false

    case errors.Is(err, relay.ErrRateLimitExceeded):
        slog.WarnContext(ctx, "local rate limit exceeded")
        return false

    case relay.IsTimeout(err):
        slog.WarnContext(ctx, "request timed out")
        return true // caller may wish to retry with a longer timeout

    case errors.Is(err, context.Canceled):
        slog.InfoContext(ctx, "request cancelled by caller")
        return false

    case errors.Is(err, context.DeadlineExceeded):
        slog.WarnContext(ctx, "context deadline exceeded")
        return false
    }

    if httpErr, ok := relay.IsHTTPError(err); ok {
        switch {
        case httpErr.StatusCode == 429:
            slog.WarnContext(ctx, "rate limited by server",
                "retry_after", httpErr.Body)
            return true
        case httpErr.StatusCode >= 500:
            slog.ErrorContext(ctx, "server error", "status", httpErr.StatusCode)
            return relay.IsRetryableError(err, resp)
        case httpErr.StatusCode >= 400:
            slog.ErrorContext(ctx, "client error", "status", httpErr.StatusCode)
            return false // 4xx is permanent
        }
    }

    slog.ErrorContext(ctx, "unknown error", "err", err)
    return false
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTimeout(5*time.Second),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    ctx := context.Background()
    resp, err := client.Execute(client.Get("/orders").WithContext(ctx))
    if handleRelayError(ctx, err, resp) {
        fmt.Println("application layer may retry")
    }
}
```

---

## Wrapping relay errors with additional context

Wrap relay errors just like any other Go error using `fmt.Errorf` with `%w`.
The wrapped sentinel remains reachable via `errors.Is`:

```go
package main

import (
    "context"
    "errors"
    "fmt"

    relay "github.com/jhonsferg/relay"
)

type OrderError struct {
    OrderID string
    Cause   error
}

func (e *OrderError) Error() string {
    return fmt.Sprintf("order %s: %v", e.OrderID, e.Cause)
}

func (e *OrderError) Unwrap() error { return e.Cause }

func fetchOrder(client *relay.Client, id string) error {
    _, err := client.Execute(client.Get("/orders/" + id))
    if err != nil {
        return &OrderError{OrderID: id, Cause: err}
    }
    return nil
}

func main() {
    client := relay.New(relay.WithBaseURL("https://api.example.com"))
    defer client.Shutdown(context.Background()) //nolint:errcheck

    err := fetchOrder(client, "ord-123")
    if err != nil {
        fmt.Println(err)

        // Sentinel errors are still reachable through the wrapper.
        if errors.Is(err, relay.ErrCircuitOpen) {
            fmt.Println("(circuit was open)")
        }
    }
}
```

---

## Custom error handlers via OnError hook

The `WithOnErrorHook` option lets you centralise error side-effects (logging,
metrics, alerting) in one place rather than repeating them at every call site:

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log/slog"
    "time"

    relay "github.com/jhonsferg/relay"
)

func newInstrumentedClient() *relay.Client {
    return relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTimeout(5*time.Second),

        relay.WithOnErrorHook(func(ctx context.Context, req *relay.Request, err error) {
            attrs := []any{
                "method", req.Method(),
                "url",    req.URL(),
            }

            switch {
            case errors.Is(err, relay.ErrCircuitOpen):
                attrs = append(attrs, "class", "circuit_open")
            case errors.Is(err, relay.ErrBulkheadFull):
                attrs = append(attrs, "class", "bulkhead_full")
            case relay.IsTimeout(err):
                attrs = append(attrs, "class", "timeout")
            case errors.Is(err, relay.ErrMaxRetriesReached):
                attrs = append(attrs, "class", "max_retries")
            default:
                attrs = append(attrs, "class", "unknown")
            }

            slog.ErrorContext(ctx, "http error", attrs...)
        }),
    )
}

func main() {
    client := newInstrumentedClient()
    defer client.Shutdown(context.Background()) //nolint:errcheck

    if _, err := client.Execute(client.Get("/users")); err != nil {
        // The hook already logged the error; just propagate.
        fmt.Println("request failed:", err)
    }
}
```

> **Warning**
> `OnErrorHook` fires for *every* error, including transient ones during
> retries. If you are tracking "final" errors only, pair the hook with a check
> for `relay.ErrMaxRetriesReached` or use the hook solely for metrics so that
> retried-and-recovered calls are not incorrectly counted as failures.
