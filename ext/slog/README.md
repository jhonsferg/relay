# ext/slog - Structured Logging for relay

This extension provides structured HTTP request and response logging for the relay HTTP client using Go's standard `log/slog` package (available since Go 1.21).

## Installation

```go
import "github.com/jhonsferg/relay/ext/slog"
```

## Usage

```go
package main

import (
    "log/slog"
    "os"

    "github.com/jhonsferg/relay"
    relayslog "github.com/jhonsferg/relay/ext/slog"
)

func main() {
    logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relayslog.WithRequestResponseLogging(logger),
    )

    req := client.Get("/users")
    resp, err := client.Execute(req)
    if err != nil {
        panic(err)
    }
    defer resp.Body().Close()
}
```

## Logging Levels

Logs are emitted at different slog levels based on HTTP response status:

- `slog.LevelInfo` - Successful responses (2xx, 3xx)
- `slog.LevelWarn` - Client errors (4xx)
- `slog.LevelError` - Server errors (5xx) and transport errors

## Structured Fields

Each log entry includes the following structured fields:

- `method` - HTTP method (GET, POST, etc.)
- `url` - Request URL
- `status_code` - HTTP response status code (for successful responses)
- `duration_ms` - Request duration in milliseconds
- `attempt` - Retry attempt number (1-based, for retry logs)
- `error` - Error message (for error logs)

## Default Logger

If no logger is provided (or `nil` is passed), the extension uses `slog.Default()`.

## Example Output

With a JSON handler:

```json
{
  "time": "2024-01-15T10:23:45.123Z",
  "level": "INFO",
  "msg": "http_response",
  "method": "GET",
  "url": "https://api.example.com/users",
  "status_code": 200,
  "duration_ms": 145
}
```

```json
{
  "time": "2024-01-15T10:23:46.456Z",
  "level": "WARN",
  "msg": "http_response",
  "method": "POST",
  "url": "https://api.example.com/users",
  "status_code": 400,
  "duration_ms": 89
}
```

```json
{
  "time": "2024-01-15T10:23:47.789Z",
  "level": "ERROR",
  "msg": "http_error",
  "method": "GET",
  "url": "https://api.example.com/users",
  "error": "dial tcp: connection refused"
}
```
