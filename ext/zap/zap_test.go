package zap_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	uberzap "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/jhonsferg/relay"
	relayzap "github.com/jhonsferg/relay/ext/zap"
)

// newObserved returns an adapter backed by an in-memory observer core so
// tests can inspect emitted log entries without writing to stderr.
func newObserved(minLevel zapcore.Level) (relay.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(minLevel)
	logger := uberzap.New(core)
	return relayzap.NewAdapter(logger), logs
}

func TestNewAdapter_AllLevelsForwarded(t *testing.T) {
	t.Parallel()

	adapter, logs := newObserved(uberzap.DebugLevel)

	adapter.Debug("debug msg")
	adapter.Info("info msg")
	adapter.Warn("warn msg")
	adapter.Error("error msg")

	if got := logs.Len(); got != 4 {
		t.Fatalf("expected 4 log entries, got %d", got)
	}

	entries := logs.All()
	want := []struct {
		msg   string
		level zapcore.Level
	}{
		{"debug msg", uberzap.DebugLevel},
		{"info msg", uberzap.InfoLevel},
		{"warn msg", uberzap.WarnLevel},
		{"error msg", uberzap.ErrorLevel},
	}
	for i, w := range want {
		if entries[i].Message != w.msg {
			t.Errorf("[%d] message = %q, want %q", i, entries[i].Message, w.msg)
		}
		if entries[i].Level != w.level {
			t.Errorf("[%d] level = %v, want %v", i, entries[i].Level, w.level)
		}
	}
}

func TestNewAdapter_KeyValuePairsBecomFields(t *testing.T) {
	t.Parallel()

	adapter, logs := newObserved(uberzap.DebugLevel)

	adapter.Info("request", "method", "GET", "status", 200, "path", "/users")

	if logs.Len() != 1 {
		t.Fatalf("expected 1 entry, got %d", logs.Len())
	}
	entry := logs.All()[0]
	if entry.Message != "request" {
		t.Errorf("message = %q, want %q", entry.Message, "request")
	}
	// SugaredLogger stores key-value pairs as individual zap.Field values.
	if len(entry.Context) != 3 {
		t.Errorf("field count = %d, want 3", len(entry.Context))
	}
}

func TestNewAdapter_LevelFiltering(t *testing.T) {
	t.Parallel()

	// Only Warn and above should be captured.
	adapter, logs := newObserved(uberzap.WarnLevel)

	adapter.Debug("dropped debug")
	adapter.Info("dropped info")
	adapter.Warn("kept warn")
	adapter.Error("kept error")

	if got := logs.Len(); got != 2 {
		t.Fatalf("expected 2 entries after level filter, got %d", got)
	}
	if logs.All()[0].Message != "kept warn" {
		t.Errorf("unexpected first message: %q", logs.All()[0].Message)
	}
}

func TestNewSugaredAdapter(t *testing.T) {
	t.Parallel()

	core, logs := observer.New(uberzap.DebugLevel)
	sugared := uberzap.New(core).Sugar()
	adapter := relayzap.NewSugaredAdapter(sugared)

	adapter.Debug("via sugared", "k", "v")
	adapter.Info("via sugared")

	if logs.Len() != 2 {
		t.Fatalf("expected 2 entries, got %d", logs.Len())
	}
}

func TestNewAdapter_IntegrationWithRelayClient(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	adapter, logs := newObserved(uberzap.DebugLevel)

	// Emit a log entry via OnBeforeRequest to verify the adapter is wired up.
	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithLogger(adapter),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithOnBeforeRequest(func(_ context.Context, req *relay.Request) error {
			adapter.Debug("before request", "url", req.URL())
			return nil
		}),
	)

	_, err := c.Execute(c.Get("/ping"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The OnBeforeRequest hook emits exactly one debug entry.
	if logs.Len() == 0 {
		t.Error("expected at least one log entry from OnBeforeRequest hook")
	}
}

func TestNewAdapter_NoArgsDoesNotPanic(t *testing.T) {
	t.Parallel()

	adapter, _ := newObserved(uberzap.DebugLevel)

	// Must not panic when no key-value args are provided.
	adapter.Debug("plain message")
	adapter.Info("plain message")
	adapter.Warn("plain message")
	adapter.Error("plain message")
}
