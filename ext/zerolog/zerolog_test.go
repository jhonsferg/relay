package zerolog_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"github.com/jhonsferg/relay"
	relayzl "github.com/jhonsferg/relay/ext/zerolog"
)

// newBufAdapter returns an adapter that writes JSON to buf.
func newBufAdapter(buf *bytes.Buffer, level zerolog.Level) relay.Logger {
	l := zerolog.New(buf).Level(level)
	return relayzl.NewAdapter(l)
}

// parseLastEntry parses the last (or only) newline-delimited JSON object from buf.
func parseLastEntry(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	data := bytes.TrimSpace(buf.Bytes())
	// Take the last line in case multiple entries were written.
	lines := bytes.Split(data, []byte("\n"))
	last := lines[len(lines)-1]
	var m map[string]any
	if err := json.Unmarshal(last, &m); err != nil {
		t.Fatalf("invalid JSON log entry %q: %v", last, err)
	}
	return m
}

func TestNewAdapter_DebugLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	adapter := newBufAdapter(&buf, zerolog.DebugLevel)

	adapter.Debug("debug message")

	entry := parseLastEntry(t, &buf)
	if entry["level"] != "debug" {
		t.Errorf("level = %v, want debug", entry["level"])
	}
	if entry["message"] != "debug message" {
		t.Errorf("message = %v, want \"debug message\"", entry["message"])
	}
}

func TestNewAdapter_InfoLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	adapter := newBufAdapter(&buf, zerolog.DebugLevel)

	adapter.Info("info message")

	entry := parseLastEntry(t, &buf)
	if entry["level"] != "info" {
		t.Errorf("level = %v, want info", entry["level"])
	}
	if entry["message"] != "info message" {
		t.Errorf("message = %v, want \"info message\"", entry["message"])
	}
}

func TestNewAdapter_WarnLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	adapter := newBufAdapter(&buf, zerolog.DebugLevel)

	adapter.Warn("warn message")

	entry := parseLastEntry(t, &buf)
	if entry["level"] != "warn" {
		t.Errorf("level = %v, want warn", entry["level"])
	}
}

func TestNewAdapter_ErrorLevel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	adapter := newBufAdapter(&buf, zerolog.DebugLevel)

	adapter.Error("error message")

	entry := parseLastEntry(t, &buf)
	if entry["level"] != "error" {
		t.Errorf("level = %v, want error", entry["level"])
	}
}

func TestNewAdapter_KeyValuePairsInOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	adapter := newBufAdapter(&buf, zerolog.DebugLevel)

	adapter.Info("request done", "method", "GET", "status", float64(200), "path", "/users")

	entry := parseLastEntry(t, &buf)
	if entry["method"] != "GET" {
		t.Errorf("method = %v, want GET", entry["method"])
	}
	// JSON numbers decode as float64.
	if entry["status"] != float64(200) {
		t.Errorf("status = %v, want 200", entry["status"])
	}
	if entry["path"] != "/users" {
		t.Errorf("path = %v, want /users", entry["path"])
	}
}

func TestNewAdapter_LevelFiltering(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	// Only Warn and above should produce output.
	adapter := newBufAdapter(&buf, zerolog.WarnLevel)

	adapter.Debug("dropped")
	adapter.Info("dropped")

	if buf.Len() != 0 {
		t.Errorf("expected no output for sub-warn messages, got %q", buf.String())
	}

	adapter.Warn("kept")
	if buf.Len() == 0 {
		t.Error("expected warn output, got nothing")
	}
}

func TestNewAdapter_NoArgsDoesNotPanic(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	adapter := newBufAdapter(&buf, zerolog.DebugLevel)

	// Must not panic when no key-value args are provided.
	adapter.Debug("plain")
	adapter.Info("plain")
	adapter.Warn("plain")
	adapter.Error("plain")
}

func TestNewAdapter_IntegrationWithRelayClient(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	adapter := newBufAdapter(&buf, zerolog.DebugLevel)

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

	if buf.Len() == 0 {
		t.Error("expected at least one log entry from OnBeforeRequest hook")
	}

	// Verify output is valid JSON.
	parseLastEntry(t, &buf)
}

func TestNewAdapter_WithContext(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	// Build a logger with static context fields.
	base := zerolog.New(&buf).With().Str("service", "test-svc").Logger()
	adapter := relayzl.NewAdapter(base)

	adapter.Info("event", "key", "val")

	entry := parseLastEntry(t, &buf)
	if entry["service"] != "test-svc" {
		t.Errorf("service = %v, want test-svc", entry["service"])
	}
	if entry["key"] != "val" {
		t.Errorf("key = %v, want val", entry["key"])
	}
}
