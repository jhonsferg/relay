# Hooks

Hooks are callbacks registered on a relay `Client` that fire at well-defined
points in the request lifecycle. They are the primary extension point for
cross-cutting concerns such as logging, metrics, request mutation, and error
enrichment - all without touching the core request/response logic.

relay provides three hook types:

| Hook | When it fires | Return value |
|------|--------------|--------------|
| `BeforeRetryHook` | Before each retry sleep | none |
| `BeforeRedirectHook` | Before each redirect is followed | `error` (stops redirect chain) |
| `OnErrorHook` | After `Execute` returns a non-nil error | none |

---

## Hook function signatures

```go
// BeforeRetryHookFunc is called before each retry sleep.
// attempt is 1-based (first retry = 1).
// httpResp and err reflect the result that triggered the retry; either may be nil.
type BeforeRetryHookFunc func(
    ctx      context.Context,
    attempt  int,
    req      *relay.Request,
    httpResp *http.Response,
    err      error,
)

// BeforeRedirectHookFunc is called before each redirect is followed.
// Returning a non-nil error stops the redirect chain; the error propagates
// as the Execute return value.
type BeforeRedirectHookFunc func(req *http.Request, via []*http.Request) error

// OnErrorHookFunc is called when Execute returns a non-nil error.
// It is intended for logging and metrics; its return value is discarded.
type OnErrorHookFunc func(ctx context.Context, req *relay.Request, err error)
```

---

## WithBeforeRetryHook

`WithBeforeRetryHook` registers a function that relay calls just before
sleeping between retry attempts. It is useful for structured logging, emitting
retry metrics, and mutating the request (for example, refreshing a short-lived
token before the next attempt).

### Basic retry logger

```go
package main

import (
    "context"
    "log/slog"
    "net/http"
    "os"
    "time"

    relay "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithBeforeRetryHook(func(
            ctx     context.Context,
            attempt int,
            req     *relay.Request,
            resp    *http.Response,
            err     error,
        ) {
            statusCode := 0
            if resp != nil {
                statusCode = resp.StatusCode
            }
            slog.InfoContext(ctx, "retrying request",
                "attempt",     attempt,
                "url",         req.URL(),
                "status_code", statusCode,
                "error",       err,
            )
        }),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    resp, err := client.Execute(client.Get("/users"))
    if err != nil {
        slog.Error("request failed", "err", err)
        os.Exit(1)
    }
    slog.Info("ok", "status", resp.StatusCode)
}
```

### Refreshing a token before retry

Some short-lived tokens (JWT, OAuth access token) expire between attempts.
Mutate the request inside a `BeforeRetryHook` to attach a fresh token:

```go
package main

import (
    "context"
    "net/http"
    "sync"

    relay "github.com/jhonsferg/relay"
)

type tokenStore struct {
    mu    sync.Mutex
    token string
}

func (ts *tokenStore) Refresh() string {
    ts.mu.Lock()
    defer ts.mu.Unlock()
    // In production this would call the token endpoint.
    ts.token = "new-access-token-" + "refreshed"
    return ts.token
}

func main() {
    store := &tokenStore{token: "initial-token"}

    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithBeforeRetryHook(func(
            ctx     context.Context,
            attempt int,
            req     *relay.Request,
            resp    *http.Response,
            err     error,
        ) {
            // Only refresh when the previous response was a 401.
            if resp != nil && resp.StatusCode == http.StatusUnauthorized {
                fresh := store.Refresh()
                req.Header("Authorization", "Bearer "+fresh)
            }
        }),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    _, _ = client.Execute(
        client.Get("/protected").Header("Authorization", "Bearer "+store.token),
    )
}
```

### Emitting retry metrics

```go
package main

import (
    "context"
    "net/http"
    "sync/atomic"

    relay "github.com/jhonsferg/relay"
)

var retryCounter int64

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithBeforeRetryHook(func(
            _ context.Context,
            _ int,
            _ *relay.Request,
            _ *http.Response,
            _ error,
        ) {
            atomic.AddInt64(&retryCounter, 1)
            // In production: metrics.Inc("http_client_retries_total")
        }),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    _, _ = client.Execute(client.Get("/health"))
}
```

---

## WithBeforeRedirectHook

`WithBeforeRedirectHook` registers a function that fires before relay follows
each redirect. Returning a non-nil error stops the redirect chain immediately
and propagates the error as the `Execute` return value.

### Logging the redirect chain

```go
package main

import (
    "context"
    "fmt"
    "log/slog"
    "net/http"

    relay "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithBeforeRedirectHook(func(req *http.Request, via []*http.Request) error {
            slog.Info("following redirect",
                "from", via[len(via)-1].URL.String(),
                "to",   req.URL.String(),
                "hops", len(via),
            )
            return nil
        }),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    _, _ = client.Execute(client.Get("/old-path"))
}
```

### Blocking cross-origin redirects

Some security policies prohibit following redirects to a different host. Use a
`BeforeRedirectHook` to enforce this at the client level:

```go
package main

import (
    "context"
    "errors"
    "net/http"

    relay "github.com/jhonsferg/relay"
)

var errCrossOriginRedirect = errors.New("cross-origin redirect blocked")

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithBeforeRedirectHook(func(req *http.Request, via []*http.Request) error {
            origin := via[0].URL.Host
            target := req.URL.Host
            if origin != target {
                return fmt.Errorf("%w: %s -> %s", errCrossOriginRedirect, origin, target)
            }
            return nil
        }),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    resp, err := client.Execute(client.Get("/maybe-redirects"))
    if errors.Is(err, errCrossOriginRedirect) {
        // Handle policy violation.
        _ = resp
    }
}
```

> **Note**
> The `via` slice holds every request that was already followed, starting with
> the original. `via[0]` is always the initial request; `via[len(via)-1]` is
> the most recent one.

### Forwarding auth headers across redirects

By default the Go HTTP client strips the `Authorization` header when following
a redirect to a different host. Explicitly re-attach it when you trust the
destination:

```go
package main

import (
    "context"
    "net/http"

    relay "github.com/jhonsferg/relay"
)

func main() {
    const token = "Bearer super-secret"

    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithBeforeRedirectHook(func(req *http.Request, via []*http.Request) error {
            // Re-attach the Authorization header that Go stripped.
            if via[0].Header.Get("Authorization") != "" {
                req.Header.Set("Authorization", via[0].Header.Get("Authorization"))
            }
            return nil
        }),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    _, _ = client.Execute(
        client.Get("/redirect-me").Header("Authorization", token),
    )
}
```

---

## WithOnErrorHook

`WithOnErrorHook` registers a function that relay calls after `Execute` returns
a non-nil error. The hook's return value is discarded; it is purely for
side-effects such as logging, alerting, or incrementing error counters.

### Structured error logging

```go
package main

import (
    "context"
    "log/slog"
    "os"

    relay "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithOnErrorHook(func(ctx context.Context, req *relay.Request, err error) {
            slog.ErrorContext(ctx, "http request failed",
                "url",    req.URL(),
                "method", req.Method(),
                "error",  err,
            )
        }),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    if _, err := client.Execute(client.Get("/users")); err != nil {
        os.Exit(1)
    }
}
```

### Sending errors to an alerting system

```go
package main

import (
    "context"
    "fmt"

    relay "github.com/jhonsferg/relay"
)

// alerting is a stand-in for any error-tracking client (Sentry, Datadog, etc.)
func alerting(msg string) { fmt.Println("ALERT:", msg) }

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithOnErrorHook(func(_ context.Context, req *relay.Request, err error) {
            if relay.IsCircuitOpen(err) {
                alerting(fmt.Sprintf("circuit open for %s", req.URL()))
            }
        }),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    _, _ = client.Execute(client.Get("/payments"))
}
```

---

## Adding multiple hooks

You can call `WithBeforeRetryHook`, `WithBeforeRedirectHook`, and
`WithOnErrorHook` multiple times. Each call **appends** to the existing slice;
hooks are never replaced.

```go
package main

import (
    "context"
    "fmt"
    "net/http"

    relay "github.com/jhonsferg/relay"
)

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),

        // First retry hook - logging.
        relay.WithBeforeRetryHook(func(_ context.Context, attempt int, req *relay.Request, _ *http.Response, _ error) {
            fmt.Printf("[hook-1] retry attempt %d for %s\n", attempt, req.URL())
        }),

        // Second retry hook - metrics.
        relay.WithBeforeRetryHook(func(_ context.Context, _ int, _ *relay.Request, _ *http.Response, _ error) {
            fmt.Println("[hook-2] incrementing retry counter")
        }),

        // First error hook - log.
        relay.WithOnErrorHook(func(_ context.Context, req *relay.Request, err error) {
            fmt.Printf("[on-error-1] %s failed: %v\n", req.URL(), err)
        }),

        // Second error hook - alert.
        relay.WithOnErrorHook(func(_ context.Context, req *relay.Request, err error) {
            fmt.Printf("[on-error-2] sending alert for %s\n", req.URL())
        }),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    _, _ = client.Execute(client.Get("/multi-hook-demo"))
}
```

---

## Hook ordering (FIFO)

relay executes all hooks of the same type in **first-in, first-out** order -
the order they were passed to `relay.New`. This is intentional and
deterministic:

```
WithBeforeRetryHook(A)  -->  registered first  -->  called first
WithBeforeRetryHook(B)  -->  registered second -->  called second
WithBeforeRetryHook(C)  -->  registered third  -->  called third
```

The same FIFO rule applies to `WithOnErrorHook` and
`WithBeforeRedirectHook`.

> **Tip**
> Put logging hooks before alerting hooks so your logs always appear before
> any alert fires. This makes debugging easier.

```go
package main

import (
    "context"
    "fmt"
    "net/http"

    relay "github.com/jhonsferg/relay"
)

func main() {
    order := []string{}

    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithBeforeRetryHook(func(_ context.Context, _ int, _ *relay.Request, _ *http.Response, _ error) {
            order = append(order, "A")
        }),
        relay.WithBeforeRetryHook(func(_ context.Context, _ int, _ *relay.Request, _ *http.Response, _ error) {
            order = append(order, "B")
        }),
        relay.WithBeforeRetryHook(func(_ context.Context, _ int, _ *relay.Request, _ *http.Response, _ error) {
            order = append(order, "C")
        }),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    _, _ = client.Execute(client.Get("/order-demo"))

    fmt.Println(order) // [A B C] on every retry
}
```

---

## Complete example: logging + metrics + error enrichment

The example below wires all three hook types together to build a
production-ready observability layer entirely outside of the request/response
path:

```go
package main

import (
    "context"
    "errors"
    "fmt"
    "log/slog"
    "net/http"
    "os"
    "sync/atomic"
    "time"

    relay "github.com/jhonsferg/relay"
)

// --- minimal in-process metrics -----------------------------------------

var (
    totalRetries   int64
    totalErrors    int64
    totalRedirects int64
)

func incRetries()   { atomic.AddInt64(&totalRetries, 1) }
func incErrors()    { atomic.AddInt64(&totalErrors, 1) }
func incRedirects() { atomic.AddInt64(&totalRedirects, 1) }

// --- main ---------------------------------------------------------------

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTimeout(5*time.Second),

        // Retry hook: log + metric.
        relay.WithBeforeRetryHook(func(
            ctx     context.Context,
            attempt int,
            req     *relay.Request,
            resp    *http.Response,
            err     error,
        ) {
            incRetries()
            code := 0
            if resp != nil {
                code = resp.StatusCode
            }
            slog.WarnContext(ctx, "retrying",
                "attempt", attempt,
                "url",     req.URL(),
                "status",  code,
                "err",     err,
            )
        }),

        // Redirect hook: metric + cross-origin guard.
        relay.WithBeforeRedirectHook(func(req *http.Request, via []*http.Request) error {
            incRedirects()
            slog.Info("redirect",
                "from", via[len(via)-1].URL.String(),
                "to",   req.URL.String(),
            )
            return nil
        }),

        // Error hook: metric + structured log.
        relay.WithOnErrorHook(func(ctx context.Context, req *relay.Request, err error) {
            incErrors()

            attrs := []any{
                "url",   req.URL(),
                "error", err,
            }
            switch {
            case relay.IsCircuitOpen(err):
                attrs = append(attrs, "class", "circuit_open")
            case relay.IsTimeout(err):
                attrs = append(attrs, "class", "timeout")
            case errors.Is(err, relay.ErrMaxRetriesReached):
                attrs = append(attrs, "class", "max_retries")
            default:
                attrs = append(attrs, "class", "unknown")
            }
            slog.ErrorContext(ctx, "request failed", attrs...)
        }),
    )
    defer client.Shutdown(context.Background()) //nolint:errcheck

    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    _, err := client.Execute(client.Get("/users").WithContext(ctx))
    if err != nil {
        fmt.Fprintln(os.Stderr, "fatal:", err)
        os.Exit(1)
    }

    fmt.Printf("retries=%d redirects=%d errors=%d\n",
        atomic.LoadInt64(&totalRetries),
        atomic.LoadInt64(&totalRedirects),
        atomic.LoadInt64(&totalErrors),
    )
}
```

> **Warning**
> Hook functions must be goroutine-safe if the client is used from multiple
> goroutines concurrently. Use `sync/atomic` or a mutex to guard any shared
> mutable state inside a hook.
