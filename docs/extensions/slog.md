# Structured Logging Extension (slog)

The `ext/slog` extension logs every HTTP request and response using Go's standard `log/slog` package, providing structured, queryable log entries for observability.

## Installation

```bash
go get github.com/jhonsferg/relay/ext/slog
```

## Quick Start

```go
import relayslog "github.com/jhonsferg/relay/ext/slog"

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relayslog.Middleware(nil), // uses slog.Default()
)
```

## Custom Logger

```go
import (
    "log/slog"
    "os"
    relayslog "github.com/jhonsferg/relay/ext/slog"
)

logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relayslog.Middleware(logger),
)
```

## Log Levels

Responses are logged at different levels based on HTTP status:

| Status Range | Log Level | Example |
|-------------|-----------|---------|
| 2xx, 3xx | `INFO` | Successful responses |
| 4xx | `WARN` | Client errors (not found, unauthorized) |
| 5xx | `ERROR` | Server errors |
| Transport error | `ERROR` | Network failures, timeouts |

## Log Fields

Each log entry contains structured fields:

| Field | Type | Description |
|-------|------|-------------|
| `method` | string | HTTP method (GET, POST, ...) |
| `url` | string | Full request URL |
| `status_code` | int | HTTP response status code |
| `duration_ms` | float64 | Request duration in milliseconds |
| `attempt` | int | Attempt number (1 for first, 2+ for retries) |
| `error` | string | Error message (only on transport errors) |

## Example Log Output

```json
{"time":"2026-04-05T10:00:00Z","level":"INFO","msg":"HTTP request","method":"GET","url":"https://api.example.com/users","status_code":200,"duration_ms":45.2,"attempt":1}
{"time":"2026-04-05T10:00:01Z","level":"WARN","msg":"HTTP request","method":"GET","url":"https://api.example.com/users/999","status_code":404,"duration_ms":12.1,"attempt":1}
{"time":"2026-04-05T10:00:02Z","level":"ERROR","msg":"HTTP request","method":"POST","url":"https://api.example.com/data","status_code":500,"duration_ms":230.5,"attempt":3}
```

## Retry Logging

When retries are configured, each attempt is logged with its attempt number:

```go
client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relay.WithRetry(&relay.RetryConfig{MaxAttempts: 3}),
    relayslog.Middleware(logger),
)
```

```json
{"level":"ERROR","msg":"HTTP request","method":"GET","url":"/flaky","status_code":503,"attempt":1}
{"level":"ERROR","msg":"HTTP request","method":"GET","url":"/flaky","status_code":503,"attempt":2}
{"level":"INFO", "msg":"HTTP request","method":"GET","url":"/flaky","status_code":200,"attempt":3}
```

## Filtering Log Level

Use `slog.HandlerOptions` to control which levels are emitted:

```go
// Only log warnings and errors (skip successful requests)
logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelWarn,
}))
```
