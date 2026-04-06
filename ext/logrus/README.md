# relay/ext/logrus

> **Deprecated** — use [`relay/ext/slog`](../slog) instead.

This package provides a [logrus](https://github.com/sirupsen/logrus) adapter for the relay `Logger` interface.

## Migration

Replace this adapter with the standard-library `log/slog` adapter to remove the logrus dependency:

```go
// Before
import relaylogrus "github.com/jhonsferg/relay/ext/logrus"

client := relay.New(
    relay.WithLogger(relaylogrus.NewAdapter(logrus.New())),
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
