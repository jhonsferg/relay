package slog

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
)

type testLogHandler struct {
	records []slog.Record
}

func (h *testLogHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}

func (h *testLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

func (h *testLogHandler) WithGroup(string) slog.Handler { return h }

func (h *testLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func TestSuccessfulRequestLogsAtInfoLevel(t *testing.T) {
	handler := &testLogHandler{}
	logger := slog.New(handler)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	client := relay.New(
		relay.WithBaseURL(server.URL),
		WithRequestResponseLogging(logger),
	)

	req := client.Get("/test")
	_, err := client.Execute(req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(handler.records) == 0 {
		t.Fatal("Expected at least one log record")
	}

	record := handler.records[0]
	if record.Level != slog.LevelInfo {
		t.Errorf("Expected level Info, got %v", record.Level)
	}

	if record.Message != "http_response" {
		t.Errorf("Expected message 'http_response', got %q", record.Message)
	}

	attrs := extractAttrs(&record)
	statusCode, ok := attrs["status_code"].(int64)
	if !ok {
		statusCode2, ok2 := attrs["status_code"].(int)
		if ok2 {
			statusCode = int64(statusCode2)
		} else {
			t.Fatalf("Expected status_code to be an int, got %T: %v", attrs["status_code"], attrs["status_code"])
		}
	}
	if statusCode != 200 {
		t.Errorf("Expected status_code 200, got %d", statusCode)
	}
}

func TestClientErrorLogsAtWarnLevel(t *testing.T) {
	handler := &testLogHandler{}
	logger := slog.New(handler)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "bad request"}`))
	}))
	defer server.Close()

	client := relay.New(
		relay.WithBaseURL(server.URL),
		relay.WithDisableRetry(),
		WithRequestResponseLogging(logger),
	)

	req := client.Get("/test")
	_, err := client.Execute(req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var warnRecord *slog.Record
	for i, record := range handler.records {
		if record.Level == slog.LevelWarn {
			warnRecord = &handler.records[i]
			break
		}
	}

	if warnRecord == nil {
		t.Fatal("Expected a warn level log record for 4xx status")
	}

	attrs := extractAttrs(warnRecord)
	statusCode, ok := attrs["status_code"].(int64)
	if !ok {
		statusCode2, ok2 := attrs["status_code"].(int)
		if ok2 {
			statusCode = int64(statusCode2)
		} else {
			t.Fatalf("Expected status_code to be an int, got %T", attrs["status_code"])
		}
	}
	if statusCode != 400 {
		t.Errorf("Expected status_code 400, got %d", statusCode)
	}
}

func TestServerErrorLogsAtErrorLevel(t *testing.T) {
	handler := &testLogHandler{}
	logger := slog.New(handler)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	client := relay.New(
		relay.WithBaseURL(server.URL),
		relay.WithDisableRetry(),
		WithRequestResponseLogging(logger),
	)

	req := client.Get("/test")
	_, err := client.Execute(req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var errorRecord *slog.Record
	for i, record := range handler.records {
		if record.Level == slog.LevelError && record.Message == "http_response" {
			errorRecord = &handler.records[i]
			break
		}
	}

	if errorRecord == nil {
		t.Fatal("Expected an error level log record for 5xx status")
	}

	attrs := extractAttrs(errorRecord)
	statusCode, ok := attrs["status_code"].(int64)
	if !ok {
		statusCode2, ok2 := attrs["status_code"].(int)
		if ok2 {
			statusCode = int64(statusCode2)
		} else {
			t.Fatalf("Expected status_code to be an int, got %T", attrs["status_code"])
		}
	}
	if statusCode != 500 {
		t.Errorf("Expected status_code 500, got %d", statusCode)
	}
}

func TestTransportErrorLogsAtErrorLevel(t *testing.T) {
	handler := &testLogHandler{}
	logger := slog.New(handler)

	client := relay.New(
		relay.WithBaseURL("http://localhost:1"),
		relay.WithDisableRetry(),
		relay.WithTimeout(100*time.Millisecond),
		WithRequestResponseLogging(logger),
	)

	req := client.Get("/test")
	_, err := client.Execute(req)
	if err == nil {
		t.Fatal("Expected Execute to fail with transport error")
	}

	var errorRecord *slog.Record
	for i, record := range handler.records {
		if record.Level == slog.LevelError && record.Message == "http_error" {
			errorRecord = &handler.records[i]
			break
		}
	}

	if errorRecord == nil {
		t.Fatal("Expected an error level log record for transport error")
	}

	attrs := extractAttrs(errorRecord)
	if _, ok := attrs["error"]; !ok {
		t.Error("Expected error field in log record")
	}
}

func TestCustomLoggerIsUsed(t *testing.T) {
	handler := &testLogHandler{}
	logger := slog.New(handler)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	client := relay.New(
		relay.WithBaseURL(server.URL),
		WithRequestResponseLogging(logger),
	)

	req := client.Get("/test")
	_, err := client.Execute(req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(handler.records) == 0 {
		t.Fatal("Expected custom logger to be used")
	}
}

func TestLogFieldsContainMethodAndURL(t *testing.T) {
	handler := &testLogHandler{}
	logger := slog.New(handler)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	client := relay.New(
		relay.WithBaseURL(server.URL),
		WithRequestResponseLogging(logger),
	)

	req := client.Post("/api/test")
	_, err := client.Execute(req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	record := handler.records[0]
	attrs := extractAttrs(&record)

	if attrs["method"] != "POST" {
		t.Errorf("Expected method POST, got %v", attrs["method"])
	}

	if !slices.Contains([]string{
		server.URL + "/api/test",
		server.URL + "/api/test/",
	}, fmt.Sprintf("%v", attrs["url"])) {
		t.Errorf("Expected URL to contain /api/test, got %v", attrs["url"])
	}
}

func TestLogFieldsContainDuration(t *testing.T) {
	handler := &testLogHandler{}
	logger := slog.New(handler)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := relay.New(
		relay.WithBaseURL(server.URL),
		WithRequestResponseLogging(logger),
	)

	req := client.Get("/test")
	_, err := client.Execute(req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	record := handler.records[0]
	attrs := extractAttrs(&record)

	duration, ok := attrs["duration_ms"].(int64)
	if !ok {
		duration2, ok2 := attrs["duration_ms"].(int)
		if ok2 {
			duration = int64(duration2)
		} else {
			t.Fatalf("Expected duration_ms to be an int, got %T", attrs["duration_ms"])
		}
	}

	if duration < 5 {
		t.Errorf("Expected duration_ms >= 5, got %d", duration)
	}
}

func TestDefaultLoggerIsUsedWhenNilIsProvided(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := relay.New(
		relay.WithBaseURL(server.URL),
		WithRequestResponseLogging(nil),
	)

	req := client.Get("/test")
	_, err := client.Execute(req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
}

func extractAttrs(record *slog.Record) map[string]any {
	attrs := make(map[string]any)
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Any()
		return true
	})
	return attrs
}
