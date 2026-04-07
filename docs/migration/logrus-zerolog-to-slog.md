# Migrating from ext/logrus or ext/zerolog to ext/slog

`ext/logrus` and `ext/zerolog` are deprecated as of relay v0.x and will be removed in v2.0.

## Why?

Go 1.21 introduced `log/slog` as the standard structured logging package. The relay core module
already ships a built-in `relay.SlogAdapter` that wraps any `*slog.Logger`, making third-party
logging adapters unnecessary for most use-cases.

Switching to `log/slog` means:

- No extra dependency (`github.com/sirupsen/logrus` or `github.com/rs/zerolog`)
- Works with any `slog.Handler` (JSON, text, or custom)
- Consistent key/value structured logging across the Go ecosystem

## Migration steps

### Option A — relay.SlogAdapter (direct drop-in)

This is the simplest path if you only need relay's internal logger.

**Before (logrus):**

```go
import (
    "github.com/sirupsen/logrus"
    "github.com/jhonsferg/relay"
    relaylogrus "github.com/jhonsferg/relay/ext/logrus"
)

log := logrus.New()
log.SetFormatter(&logrus.JSONFormatter{})

client := relay.New(
    relay.WithLogger(relaylogrus.NewAdapter(log)),
)
```

**Before (zerolog):**

```go
import (
    "os"
    "github.com/rs/zerolog"
    "github.com/jhonsferg/relay"
    relayzl "github.com/jhonsferg/relay/ext/zerolog"
)

logger := zerolog.New(os.Stderr).With().Timestamp().Logger()

client := relay.New(
    relay.WithLogger(relayzl.NewAdapter(logger)),
)
```

**After (slog — same for both):**

```go
import (
    "log/slog"
    "os"
    "github.com/jhonsferg/relay"
)

logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
    Level: slog.LevelDebug,
}))

client := relay.New(
    relay.WithLogger(relay.SlogAdapter(logger)),
)
```

To keep using the default logger:

```go
client := relay.New(
    relay.WithLogger(relay.SlogAdapter(slog.Default())),
)
```

### Option B — ext/slog middleware (request/response logging)

If you want structured per-request/response logs (including HTTP status codes, duration, and
retry events), use `ext/slog` which provides an `Option`-based middleware:

```go
import (
    "log/slog"
    "os"
    "github.com/jhonsferg/relay"
    relayslog "github.com/jhonsferg/relay/ext/slog"
)

logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))

client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relayslog.WithRequestResponseLogging(logger),
)
```

This emits structured log entries for every HTTP response, retry, and transport error.

### Preserving static fields (logrus Entry equivalent)

Logrus users sometimes use `logrus.WithFields` to attach static context to every log line.
With `slog`, use `slog.Logger.With`:

```go
// Before (logrus Entry)
entry := logrus.WithFields(logrus.Fields{
    "service": "order-service",
    "version": "2.1.0",
})
client := relay.New(relay.WithLogger(relaylogrus.NewEntryAdapter(entry)))

// After (slog.Logger.With)
logger := slog.Default().With(
    "service", "order-service",
    "version", "2.1.0",
)
client := relay.New(relay.WithLogger(relay.SlogAdapter(logger)))
```

## Removing the dependency

After migrating, remove the old module from your `go.mod`:

```bash
# If you used ext/logrus
go get github.com/jhonsferg/relay/ext/logrus@none

# If you used ext/zerolog
go get github.com/jhonsferg/relay/ext/zerolog@none

# Tidy up
go mod tidy
```

## Timeline

| Version | Status |
|---------|--------|
| v0.x    | Deprecated (functional, no new features) |
| v2.0    | **Removed** |
