package relay

import (
	"io"
	"log/slog"
	"os"
)

// Logger is the interface used by relay for internal structured logging.
// Compatible with log/slog.Logger and most popular logging libraries.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// noopLogger discards all log output. It is the default logger.
type noopLogger struct{}

func (noopLogger) Debug(_ string, _ ...any) {}
func (noopLogger) Info(_ string, _ ...any)  {}
func (noopLogger) Warn(_ string, _ ...any)  {}
func (noopLogger) Error(_ string, _ ...any) {}

// NoopLogger returns a Logger that silently discards all messages.
// This is the default when no logger is configured.
func NoopLogger() Logger { return noopLogger{} }

// slogAdapter wraps a *slog.Logger to satisfy the Logger interface.
type slogAdapter struct {
	l *slog.Logger
}

func (a *slogAdapter) Debug(msg string, args ...any) { a.l.Debug(msg, args...) }
func (a *slogAdapter) Info(msg string, args ...any)  { a.l.Info(msg, args...) }
func (a *slogAdapter) Warn(msg string, args ...any)  { a.l.Warn(msg, args...) }
func (a *slogAdapter) Error(msg string, args ...any) { a.l.Error(msg, args...) }

// SlogAdapter wraps an existing *slog.Logger so it can be passed to
// [WithLogger]. Use this when your application already has a configured slog
// instance.
func SlogAdapter(l *slog.Logger) Logger {
	return &slogAdapter{l: l}
}

// NewDefaultLogger creates a new slog-backed Logger that writes to stderr at
// the given minimum level. Suitable for quick setups and tests.
func NewDefaultLogger(level slog.Level) Logger {
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	return &slogAdapter{l: slog.New(h)}
}

// newDiscardLogger creates a logger that writes to io.Discard (used in tests).
func newDiscardLogger() Logger {
	h := slog.NewTextHandler(io.Discard, nil)
	return &slogAdapter{l: slog.New(h)}
}
