# relay/ext/logrus

> **Deprecated** — use [`relay.SlogAdapter`](https://pkg.go.dev/github.com/jhonsferg/relay#SlogAdapter) or [`relay/ext/slog`](../slog) instead.

This package provides a [logrus](https://github.com/sirupsen/logrus) adapter for the relay `Logger` interface.

## Migration

Replace this adapter with the standard-library `log/slog` adapter to remove the logrus dependency.

### Using the built-in relay.SlogAdapter (simplest)

```go
// Before
import relaylogrus "github.com/jhonsferg/relay/ext/logrus"

client := relay.New(
    relay.WithLogger(relaylogrus.NewAdapter(logrus.New())),
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
