# relay/ext/zerolog

> **Deprecated** — use [`relay.SlogAdapter`](https://pkg.go.dev/github.com/jhonsferg/relay#SlogAdapter) or [`relay/ext/slog`](../slog) instead.

This package provides a [zerolog](https://github.com/rs/zerolog) adapter for the relay `Logger` interface.

## Migration

Replace this adapter with the standard-library `log/slog` adapter to remove the zerolog dependency.

### Using the built-in relay.SlogAdapter (simplest)

```go
// Before
import relayzl "github.com/jhonsferg/relay/ext/zerolog"

client := relay.New(
    relay.WithLogger(relayzl.NewAdapter(zerolog.New(os.Stderr))),
)

// After — uses relay.SlogAdapter built into the core module
import (
    "log/slog"
    "github.com/jhonsferg/relay"
)

client := relay.New(
    relay.WithLogger(relay.SlogAdapter(slog.Default())),
)
```

### Using ext/slog for full request/response middleware logging

```go
import (
    "log/slog"
    relayslog "github.com/jhonsferg/relay/ext/slog"
)

client := relay.New(
    relayslog.WithRequestResponseLogging(slog.Default()),
)
```

This package will not be removed before relay v2.0.
