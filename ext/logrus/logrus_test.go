package logrus_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/jhonsferg/relay"
	relaylogrus "github.com/jhonsferg/relay/ext/logrus"
)

func newLogger(buf *bytes.Buffer) *logrus.Logger {
	l := logrus.New()
	l.SetOutput(buf)
	l.SetFormatter(&logrus.JSONFormatter{})
	l.SetLevel(logrus.TraceLevel)
	return l
}

func lastEntry(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	var m map[string]any
	if err := json.Unmarshal(lines[len(lines)-1], &m); err != nil {
		t.Fatalf("failed to parse log line: %v", err)
	}
	return m
}

func TestNewAdapter_AllLevels(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf)
	adapter := relaylogrus.NewAdapter(l)

	adapter.Debug("debug msg", "k", "v")
	adapter.Info("info msg", "k", "v")
	adapter.Warn("warn msg", "k", "v")
	adapter.Error("error msg", "k", "v")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 4 {
		t.Fatalf("expected 4 log lines, got %d", len(lines))
	}

	levels := []string{"debug", "info", "warning", "error"}
	msgs := []string{"debug msg", "info msg", "warn msg", "error msg"}
	for i, line := range lines {
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if m["level"] != levels[i] {
			t.Errorf("line %d: level = %q, want %q", i, m["level"], levels[i])
		}
		if m["msg"] != msgs[i] {
			t.Errorf("line %d: msg = %q, want %q", i, m["msg"], msgs[i])
		}
		if m["k"] != "v" {
			t.Errorf("line %d: k = %q, want %q", i, m["k"], "v")
		}
	}
}

func TestNewAdapter_KeyValuePairs(t *testing.T) {
	var buf bytes.Buffer
	adapter := relaylogrus.NewAdapter(newLogger(&buf))

	adapter.Info("test", "attempt", 3, "wait_ms", 400, "url", "https://example.com")

	m := lastEntry(t, &buf)
	if m["attempt"] != float64(3) {
		t.Errorf("attempt = %v, want 3", m["attempt"])
	}
	if m["wait_ms"] != float64(400) {
		t.Errorf("wait_ms = %v, want 400", m["wait_ms"])
	}
	if m["url"] != "https://example.com" {
		t.Errorf("url = %v, want https://example.com", m["url"])
	}
}

func TestNewAdapter_NoArgs(t *testing.T) {
	var buf bytes.Buffer
	adapter := relaylogrus.NewAdapter(newLogger(&buf))
	// Must not panic with zero args.
	adapter.Info("plain message")
	m := lastEntry(t, &buf)
	if m["msg"] != "plain message" {
		t.Errorf("msg = %q, want %q", m["msg"], "plain message")
	}
}

func TestNewEntryAdapter_InheritsFields(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf)
	entry := l.WithFields(logrus.Fields{
		"service": "payments",
		"region":  "us-east-1",
	})
	adapter := relaylogrus.NewEntryAdapter(entry)

	adapter.Info("transaction processed", "tx_id", "tx-123")

	m := lastEntry(t, &buf)
	if m["service"] != "payments" {
		t.Errorf("service = %q, want payments", m["service"])
	}
	if m["region"] != "us-east-1" {
		t.Errorf("region = %q, want us-east-1", m["region"])
	}
	if m["tx_id"] != "tx-123" {
		t.Errorf("tx_id = %q, want tx-123", m["tx_id"])
	}
}

func TestNewAdapter_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf)
	l.SetLevel(logrus.WarnLevel) // suppress debug and info

	adapter := relaylogrus.NewAdapter(l)
	adapter.Debug("should not appear")
	adapter.Info("should not appear")
	adapter.Warn("should appear")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("expected 1 log line, got %d: %s", len(lines), buf.Bytes())
	}
	var m map[string]any
	json.Unmarshal(lines[0], &m) //nolint:errcheck
	if m["level"] != "warning" {
		t.Errorf("level = %q, want warning", m["level"])
	}
}

func TestIntegration_RelayClientLogsWithLogrus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	l := newLogger(&buf)

	client := relay.New(
		relay.WithLogger(relaylogrus.NewAdapter(l)),
		relay.WithRetry(&relay.RetryConfig{
			MaxAttempts:     2,
			RetryableStatus: []int{500},
		}),
	)

	client.Execute(client.Get(srv.URL)) //nolint:errcheck
	// Just verify the client uses the adapter without panicking.
}
