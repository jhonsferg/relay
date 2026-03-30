package relay

import (
	"log/slog"
	"testing"
)

func TestNoopLogger_AllMethods(t *testing.T) {
	t.Parallel()
	l := NoopLogger()
	// Should not panic.
	l.Debug("debug msg", "key", "val")
	l.Info("info msg")
	l.Warn("warn msg", "count", 3)
	l.Error("error msg", "err", "oops")
}

func TestSlogAdapter_AllMethods(t *testing.T) {
	t.Parallel()
	l := newDiscardLogger()
	// Should not panic.
	l.Debug("debug", "k", "v")
	l.Info("info")
	l.Warn("warn")
	l.Error("error")
}

func TestSlogAdapter_WrapsLogger(t *testing.T) {
	t.Parallel()
	base := slog.Default()
	l := SlogAdapter(base)
	if l == nil {
		t.Fatal("SlogAdapter returned nil")
	}
	// Should not panic.
	l.Info("test info")
}

func TestNewDefaultLogger_ReturnsLogger(t *testing.T) {
	t.Parallel()
	l := NewDefaultLogger(slog.LevelError)
	if l == nil {
		t.Fatal("NewDefaultLogger returned nil")
	}
	// Should not panic (writes to stderr but only at Error+).
	l.Debug("suppressed")
	l.Error("visible error")
}

func TestWithLogger_ConfiguresLogger(t *testing.T) {
	t.Parallel()
	l := NoopLogger()
	c := New(WithLogger(l))
	if c.config.Logger != l {
		t.Error("WithLogger did not set logger in config")
	}
}
