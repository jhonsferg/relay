# Sentry Error Tracking Extension

The `sentry` extension integrates relay with Sentry for automatic error and performance tracking. It captures transport-level errors and HTTP 5xx responses as Sentry events, attaches request context to each event, and scrubs sensitive headers before sending data to Sentry.

## Installation

```bash
go get github.com/jhonsferg/relay/ext/sentry
```

## Import

```go
import relaysentry "github.com/jhonsferg/relay/ext/sentry"
```

## Quick Start

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/jhonsferg/relay"
    relaysentry "github.com/jhonsferg/relay/ext/sentry"

    "github.com/getsentry/sentry-go"
)

func main() {
    if err := sentry.Init(sentry.ClientOptions{
        Dsn:              "https://examplePublicKey@o0.ingest.sentry.io/0",
        TracesSampleRate: 1.0,
    }); err != nil {
        log.Fatalf("sentry.Init: %v", err)
    }
    defer sentry.Flush(2 * time.Second)

    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relaysentry.WithSentry(sentry.CurrentHub()),
    )
    if err != nil {
        log.Fatalf("relay.New: %v", err)
    }
    defer client.Close()

    ctx := context.Background()
    resp, err := client.Get(ctx, "/users/1")
    if err != nil {
        log.Printf("request failed: %v", err)
        return
    }
    defer resp.Body.Close()
    log.Printf("status: %d", resp.StatusCode)
}
```

## API Reference

### `relaysentry.WithSentry(hub)`

```go
func WithSentry(hub *sentry.Hub, opts ...SentryOption) relay.Option
```

**Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `hub` | `*sentry.Hub` | The Sentry hub to capture events into. Use `sentry.CurrentHub()` for the default hub, or create an isolated hub with `hub.Clone()`. |
| `opts` | `...SentryOption` | Optional configuration described below. |

### Sentry Options

| Option | Signature | Default | Description |
|--------|-----------|---------|-------------|
| `WithCaptureStatusCodes` | `func(code int) bool` | `code >= 500` | Controls which status codes trigger a Sentry event. |
| `WithURLGrouper` | `func(u *url.URL) string` | path only | Groups events by a normalised URL pattern. |
| `WithBeforeSend` | `func(e *sentry.Event) *sentry.Event` | identity | Mutates or drops events before capture. |
| `WithScrubHeaders` | `[]string` | see below | Header names whose values are replaced with `[redacted]`. |
| `WithCapturePanics` | `bool` | `false` | Recover panics inside `RoundTrip` and send them as fatal events. |
| `WithPerformanceTracking` | `bool` | `true` | Create Sentry transactions/spans for each request. |

## Capturing 5xx Errors Automatically

By default, any response with a status code of 500 or higher triggers a Sentry event. The event includes:

- The request method and URL (with sensitive headers scrubbed)
- The response status code
- The first 1024 bytes of the response body (as extra context)
- The active Sentry scope/user from the context

No code changes are required in the calling code - simply attach the extension and every 5xx response is captured.

```go
client, err := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relaysentry.WithSentry(sentry.CurrentHub()),
)
if err != nil {
    log.Fatalf("relay.New: %v", err)
}

// This 503 response is automatically sent to Sentry.
resp, err := client.Get(ctx, "/checkout")
if err != nil {
    // Transport errors (DNS failures, timeouts) are also captured.
    return
}
defer resp.Body.Close()
```

### Custom Status Code Selector

To also capture 4xx responses (e.g., for a service that should never return 404):

```go
relaysentry.WithCaptureStatusCodes(func(code int) bool {
    return code == 404 || code >= 500
})
```

To capture only 503 and 504 (e.g., for a circuit-breaker alerting scenario):

```go
relaysentry.WithCaptureStatusCodes(func(code int) bool {
    return code == 503 || code == 504
})
```

## Event Grouping by URL Pattern

Sentry groups events by their "fingerprint". Without configuration, each unique URL (including path parameters like user IDs) creates a separate issue in Sentry, leading to issue sprawl.

Use `WithURLGrouper` to normalise the URL before it is used as the fingerprint:

```go
import (
    "net/url"
    "regexp"
)

var (
    uuidPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
    intPattern  = regexp.MustCompile(`/\d+`)
)

relaysentry.WithURLGrouper(func(u *url.URL) string {
    path := uuidPattern.ReplaceAllString(u.Path, "{uuid}")
    path = intPattern.ReplaceAllString(path, "/{id}")
    return u.Host + path
})
```

With this grouper:
- `/users/123/orders/456` groups as `/users/{id}/orders/{id}`
- `/users/abc-def-123.../profile` groups as `/users/{uuid}/profile`
- All variants of the same resource pattern share a single Sentry issue.

## PII Scrubbing

The extension scrubs sensitive headers before including request data in a Sentry event. The following headers are redacted by default:

| Header | Replacement |
|--------|-------------|
| `Authorization` | `[redacted]` |
| `Cookie` | `[redacted]` |
| `Set-Cookie` | `[redacted]` |
| `X-Api-Key` | `[redacted]` |
| `X-Auth-Token` | `[redacted]` |

Add extra headers with `WithScrubHeaders`:

```go
relaysentry.WithScrubHeaders([]string{
    "Authorization",
    "Cookie",
    "X-Api-Key",
    "X-Auth-Token",
    "X-Session-Token",
    "X-Internal-Secret",
})
```

The provided list **replaces** the default list, so always include the default headers when extending it.

### Advanced: BeforeSend Hook

For complete control over what reaches Sentry, use `WithBeforeSend`. Return `nil` to drop the event entirely:

```go
relaysentry.WithBeforeSend(func(e *sentry.Event) *sentry.Event {
    // Drop events for known non-critical paths.
    if req, ok := e.Extra["request"].(*http.Request); ok {
        if req.URL.Path == "/healthz" {
            return nil // suppress health check errors
        }
    }

    // Scrub query parameters containing tokens.
    for _, exc := range e.Exception {
        // Remove token= query parameters from stack frame file paths.
        _ = exc
    }

    return e
})
```

## Complete Example with sentry.Init and Hub

This example shows a production-ready setup with:
- Sentry SDK initialised with environment and release tags
- An isolated hub per-client to avoid polluting the global scope
- URL grouping and extra header scrubbing
- Performance tracing enabled

```go
package main

import (
    "context"
    "log"
    "net/url"
    "regexp"
    "time"

    "github.com/jhonsferg/relay"
    relaysentry "github.com/jhonsferg/relay/ext/sentry"

    "github.com/getsentry/sentry-go"
)

var pathParamRE = regexp.MustCompile(`/\d+`)

func main() {
    err := sentry.Init(sentry.ClientOptions{
        Dsn:         "https://examplePublicKey@o0.ingest.sentry.io/0",
        Environment: "production",
        Release:     "my-app@1.4.2",

        // Sample 20% of transactions for performance monitoring.
        TracesSampleRate: 0.2,

        // Attach stack traces to all events, not just panics.
        AttachStacktrace: true,

        // Ignore network errors that are clearly transient.
        IgnoreErrors: []string{
            "context canceled",
            "context deadline exceeded",
        },
    })
    if err != nil {
        log.Fatalf("sentry.Init: %v", err)
    }
    defer sentry.Flush(5 * time.Second)

    // Clone the hub so this client has its own scope.
    hub := sentry.CurrentHub().Clone()
    hub.Scope().SetTag("component", "api-client")

    client, err := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithTimeout(30),
        relay.WithRetry(3, relay.ExponentialBackoff(100*time.Millisecond, 2*time.Second)),
        relaysentry.WithSentry(
            hub,
            relaysentry.WithURLGrouper(func(u *url.URL) string {
                return pathParamRE.ReplaceAllString(u.Path, "/{id}")
            }),
            relaysentry.WithScrubHeaders([]string{
                "Authorization",
                "Cookie",
                "X-Api-Key",
                "X-Auth-Token",
                "X-Session-Token",
            }),
            relaysentry.WithCaptureStatusCodes(func(code int) bool {
                return code >= 500
            }),
            relaysentry.WithPerformanceTracking(true),
        ),
    )
    if err != nil {
        log.Fatalf("relay.New: %v", err)
    }
    defer client.Close()

    // Set per-request scope using context.
    ctx := context.Background()

    // Start a Sentry transaction to act as the parent performance span.
    tx := sentry.StartTransaction(ctx, "process-batch",
        sentry.WithTransactionSource(sentry.SourceTask),
    )
    defer tx.Finish()

    ids := []int{1, 2, 3, 4, 5}
    for _, id := range ids {
        path := fmt.Sprintf("/orders/%d", id)
        resp, err := client.Get(tx.Context(), path)
        if err != nil {
            log.Printf("GET %s failed: %v", path, err)
            continue
        }
        resp.Body.Close()
        log.Printf("GET %s -> %d", path, resp.StatusCode)
    }
}
```

## Context-Aware Scope

The extension reads the Sentry hub from the request context if one is present. This means you can set per-request user context using `sentry.SetHubOnContext`:

```go
func handleCheckout(w http.ResponseWriter, r *http.Request) {
    userID := getUserID(r)

    hub := sentry.CurrentHub().Clone()
    hub.Scope().SetUser(sentry.User{
        ID:    userID,
        Email: getUserEmail(r),
    })

    ctx := sentry.SetHubOnContext(r.Context(), hub)

    resp, err := client.Post(ctx, "/payments/charge", payload)
    if err != nil {
        // Any Sentry event from this request includes the user's ID and email.
        http.Error(w, "payment failed", http.StatusInternalServerError)
        return
    }
    defer resp.Body.Close()
}
```

## Performance Tracking

When `WithPerformanceTracking(true)` is enabled (the default), the extension creates a Sentry span for each request under the active transaction. The span includes:

| Attribute | Value |
|-----------|-------|
| `op` | `http.client` |
| `description` | `"GET https://api.example.com/users/1"` |
| `http.method` | `"GET"` |
| `http.url` | `"https://api.example.com/users/1"` |
| `http.status_code` | `200` |

To see these spans in Sentry, you must have an active transaction in the context (created with `sentry.StartTransaction`).

> **note**
> Performance tracking and OpenTelemetry tracing can coexist. relay runs both the Sentry span and the OTel span in parallel. They are independent and do not share context.

## See Also

- [Tracing Extension](tracing.md) - OpenTelemetry distributed tracing
- [Extensions Overview](index.md)
