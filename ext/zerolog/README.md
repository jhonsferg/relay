# relay/ext/zerolog

> **Deprecated** — use [`relay/ext/slog`](../slog) instead.

This package provides a [zerolog](https://github.com/rs/zerolog) adapter for the relay `Logger` interface.

## Migration

Replace this adapter with the standard-library `log/slog` adapter to remove the zerolog dependency:

```go
// Before
import relayzl "github.com/jhonsferg/relay/ext/zerolog"

client := relay.New(
    relay.WithLogger(relayzl.NewAdapter(zerolog.New(os.Stderr))),
)

// After
import (
    "log/slog"
    relayslog "github.com/jhonsferg/relay/ext/slog"
)

client := relay.New(
    relay.WithLogger(relayslog.NewAdapter(slog.Default())),
)
```

This package will not be removed before relay v1.0.
