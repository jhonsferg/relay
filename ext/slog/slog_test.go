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

func TestLogRetry_WithResponse(t *testing.T) {
	handler := &testLogHandler{}
	logger := slog.New(handler)

	client := relay.New(relay.WithBaseURL("http://example.com"))
	req := client.Get("/test")

	httpReq, _ := http.NewRequest(http.MethodGet, "http://example.com/redirected", nil)
	resp := &http.Response{
		StatusCode: http.StatusServiceUnavailable,
		Request:    httpReq,
	}

	logRetry(context.Background(), logger, 2, req, resp, nil)

	if len(handler.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(handler.records))
	}
	record := handler.records[0]
	if record.Level != slog.LevelError {
		t.Errorf("expected LevelError for 5xx status, got %v", record.Level)
	}
	if record.Message != "http_retry" {
		t.Errorf("expected message http_retry, got %q", record.Message)
	}

	attrs := extractAttrs(&record)
	if attrs["url"] != "http://example.com/redirected" {
		t.Errorf("expected url from httpResp.Request, got %v", attrs["url"])
	}
	if attrs["method"] != http.MethodGet {
		t.Errorf("expected method from httpResp.Request, got %v", attrs["method"])
	}
}

func TestLogRetry_WithResponse_4xxLogsAtWarn(t *testing.T) {
	handler := &testLogHandler{}
	logger := slog.New(handler)

	client := relay.New(relay.WithBaseURL("http://example.com"))
	req := client.Get("/test")

	resp := &http.Response{StatusCode: http.StatusTooManyRequests}

	logRetry(context.Background(), logger, 1, req, resp, nil)

	if len(handler.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(handler.records))
	}
	if handler.records[0].Level != slog.LevelWarn {
		t.Errorf("expected LevelWarn for 4xx status, got %v", handler.records[0].Level)
	}
}

func TestLogRetry_WithError(t *testing.T) {
	handler := &testLogHandler{}
	logger := slog.New(handler)

	client := relay.New(relay.WithBaseURL("http://example.com"))
	req := client.Get("/test")

	logRetry(context.Background(), logger, 3, req, nil, fmt.Errorf("dial tcp: connection refused"))

	if len(handler.records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(handler.records))
	}
	record := handler.records[0]
	if record.Level != slog.LevelError {
		t.Errorf("expected LevelError for transport error, got %v", record.Level)
	}
	if record.Message != "http_retry" {
		t.Errorf("expected message http_retry, got %q", record.Message)
	}

	attrs := extractAttrs(&record)
	if attrs["url"] != req.URL() {
		t.Errorf("expected url = %q, got %v", req.URL(), attrs["url"])
	}
	if attrs["method"] != req.Method() {
		t.Errorf("expected method = %q, got %v", req.Method(), attrs["method"])
	}
	if _, ok := attrs["error"]; !ok {
		t.Error("expected error field to be present")
	}
}

func TestLogError_NilErrorIsNoOp(t *testing.T) {
	handler := &testLogHandler{}
	logger := slog.New(handler)

	client := relay.New(relay.WithBaseURL("http://example.com"))
	req := client.Get("/test")

	logError(context.Background(), logger, req, nil)

	if len(handler.records) != 0 {
		t.Errorf("expected no log records for nil error, got %d", len(handler.records))
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
