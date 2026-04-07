// Package zerolog provides a github.com/rs/zerolog adapter for the relay HTTP
// client logger interface. Applications already using zerolog can pass their
// logger directly to relay without a conversion layer.
//
// Usage:
//
//	import (
//	    "os"
//	    "github.com/rs/zerolog"
//	    "github.com/jhonsferg/relay"
//	    relayzl "github.com/jhonsferg/relay/ext/zerolog"
//	)
//
//	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relay.WithLogger(relayzl.NewAdapter(logger)),
//	)
//
// relay passes structured log fields as alternating key/value pairs (the same
// convention as log/slog and go.uber.org/zap SugaredLogger). The adapter
// forwards them via zerolog's event.Fields(), which accepts a []interface{}
// slice of alternating string keys and arbitrary values.
//
// # Migration
//
// Replace this package with [relay.SlogAdapter] from the core relay module,
// which wraps any *log/slog.Logger and requires no third-party dependency:
//
//	import (
//	    "log/slog"
//	    "github.com/jhonsferg/relay"
//	)
//
//	client := relay.New(
//	    relay.WithLogger(relay.SlogAdapter(slog.Default())),
//	)
//
// Deprecated: This package is deprecated and will be removed in relay v2.0.
// Migrate to [relay.SlogAdapter] or [github.com/jhonsferg/relay/ext/slog],
// which integrate with Go's standard log/slog package (Go 1.21+).
package zerolog

import (
	"github.com/rs/zerolog"

	"github.com/jhonsferg/relay"
)

// zerologAdapter wraps a zerolog.Logger to satisfy relay.Logger.
type zerologAdapter struct {
	l zerolog.Logger
}

// NewAdapter wraps l so it can be passed to [relay.WithLogger].
// The logger is copied by value; mutations to the original after this call do
// not affect the adapter.
//
// Deprecated: Use [relay.SlogAdapter] with a *log/slog.Logger instead.
// This function will be removed in relay v2.0.
func NewAdapter(l zerolog.Logger) relay.Logger {
	return &zerologAdapter{l: l}
}

// Debug emits a debug-level event. args must be alternating string keys and
// values (e.g. "method", "GET", "status", 200).
func (a *zerologAdapter) Debug(msg string, args ...any) {
	a.l.Debug().Fields(args).Msg(msg)
}

// Info emits an info-level event.
func (a *zerologAdapter) Info(msg string, args ...any) {
	a.l.Info().Fields(args).Msg(msg)
}

// Warn emits a warn-level event.
func (a *zerologAdapter) Warn(msg string, args ...any) {
	a.l.Warn().Fields(args).Msg(msg)
}

// Error emits an error-level event.
func (a *zerologAdapter) Error(msg string, args ...any) {
	a.l.Error().Fields(args).Msg(msg)
}
