package relay_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jhonsferg/relay"
)

// testLogger captures log calls for assertion.
type testLogger struct {
	mu      sync.Mutex
	entries []testLogEntry
}

type testLogEntry struct {
	level string
	msg   string
	args  []any
}

func (l *testLogger) Debug(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, testLogEntry{"debug", msg, args})
}
func (l *testLogger) Info(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, testLogEntry{"info", msg, args})
}
func (l *testLogger) Warn(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, testLogEntry{"warn", msg, args})
}
func (l *testLogger) Error(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, testLogEntry{"error", msg, args})
}

func (l *testLogger) has(level, substr string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, e := range l.entries {
		if e.level == level && strings.Contains(e.msg, substr) {
			return true
		}
		for _, a := range e.args {
			if s, ok := a.(string); ok && strings.Contains(s, substr) {
				return true
			}
		}
	}
	return false
}

func TestWithRequestLogger_SuccessLogsDebug(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	logger := &testLogger{}
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithRequestLogger(logger),
	)

	resp, err := client.Execute(client.Get("/ping"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	if !logger.has("debug", "request") {
		t.Error("expected debug log for outbound request, got none")
	}
	if !logger.has("debug", "response") {
		t.Error("expected debug log for successful response, got none")
	}
}

func TestWithRequestLogger_ErrorLogsWarn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Return 3 identical 500 responses because relay retries on 5xx (default 3 attempts).
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	logger := &testLogger{}
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithRequestLogger(logger),
	)

	_, _ = client.Execute(client.Get("/fail")) //nolint:errcheck

	if !logger.has("warn", "response") {
		t.Error("expected warn log for 5xx response, got none")
	}
}

func TestWithRequestLogger_NilLogger_NoPanic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// WithRequestLogger(nil) should be a no-op, not panic.
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithRequestLogger(nil),
	)
	resp, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}
