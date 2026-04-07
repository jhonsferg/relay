package relay

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

func TestHAR_ExportProducesValidJSON(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    `{"ok":true}`,
	})

	rec := NewHARRecorder()
	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithHARRecording(rec),
	)

	_, err := c.Execute(c.Get(srv.URL() + "/data"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	data, err := rec.Export()
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("Export produced invalid JSON: %s", string(data))
	}
}

func TestHAR_ValidHAR12Structure(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "hello"})

	rec := NewHARRecorder()
	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithHARRecording(rec))
	_, err := c.Execute(c.Get(srv.URL() + "/test"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	data, _ := rec.Export()

	// Parse into a raw map to validate HAR 1.2 structure.
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// Top-level must have "log".
	logRaw, ok := doc["log"]
	if !ok {
		t.Fatal("HAR export missing top-level 'log' key")
	}
	logMap, ok := logRaw.(map[string]interface{})
	if !ok {
		t.Fatal("'log' should be an object")
	}

	// version must be "1.2".
	version, ok := logMap["version"].(string)
	if !ok || version != "1.2" {
		t.Errorf("expected version '1.2', got %v", logMap["version"])
	}

	// creator must be present.
	creator, ok := logMap["creator"].(map[string]interface{})
	if !ok {
		t.Fatal("HAR export missing 'creator' field")
	}
	if creator["name"] == nil {
		t.Error("creator.name should be present")
	}

	// entries must be an array.
	entries, ok := logMap["entries"].([]interface{})
	if !ok {
		t.Fatal("'entries' should be an array")
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestHAR_EntriesAreRecorded(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	for i := 0; i < 3; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "body"})
	}

	rec := NewHARRecorder()
	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithHARRecording(rec))

	for i := 0; i < 3; i++ {
		_, err := c.Execute(c.Get(srv.URL() + "/req"))
		if err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
	}

	entries := rec.Entries()
	if len(entries) != 3 {
		t.Errorf("expected 3 recorded entries, got %d", len(entries))
	}
}

func TestHAR_EntryContainsRequestAndResponse(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusCreated,
		Headers: map[string]string{"Content-Type": "text/plain"},
		Body:    "created",
	})

	rec := NewHARRecorder()
	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithHARRecording(rec))
	_, err := c.Execute(c.Post(srv.URL() + "/items").WithJSON(map[string]string{"x": "y"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entries := rec.Entries()
	if len(entries) == 0 {
		t.Fatal("expected at least 1 entry")
	}

	entry := entries[0]

	if entry.Request.Method != http.MethodPost {
		t.Errorf("expected POST method, got %q", entry.Request.Method)
	}
	if entry.Request.URL == "" {
		t.Error("entry.Request.URL should not be empty")
	}
	if entry.Response.Status != http.StatusCreated {
		t.Errorf("expected response status 201, got %d", entry.Response.Status)
	}
	if entry.Response.Content.Text != "created" {
		t.Errorf("expected response body 'created', got %q", entry.Response.Content.Text)
	}
	if entry.StartedDateTime == "" {
		t.Error("StartedDateTime should not be empty")
	}
}

func TestHAR_Reset_ClearsEntries(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	rec := NewHARRecorder()
	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithHARRecording(rec))

	_, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(rec.Entries()) == 0 {
		t.Fatal("expected entries before Reset")
	}

	rec.Reset()

	if len(rec.Entries()) != 0 {
		t.Errorf("expected 0 entries after Reset, got %d", len(rec.Entries()))
	}

	// New requests should be recorded again after Reset.
	_, err = c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute after Reset: %v", err)
	}
	if len(rec.Entries()) != 1 {
		t.Errorf("expected 1 entry after Reset+request, got %d", len(rec.Entries()))
	}
}

func TestHAR_TimingFieldsPresent(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "timing"})

	rec := NewHARRecorder()
	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithHARRecording(rec))
	_, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entries := rec.Entries()
	if len(entries) == 0 {
		t.Fatal("no entries recorded")
	}

	e := entries[0]
	if e.Time < 0 {
		t.Errorf("entry.Time should be >= 0, got %f", e.Time)
	}
	// Timings struct should have non-negative values.
	if e.Timings.Wait < 0 {
		t.Errorf("timings.wait should be >= 0, got %f", e.Timings.Wait)
	}
}

func TestHAR_ExportAfterReset_EmptyEntries(t *testing.T) {
	t.Parallel()
	rec := NewHARRecorder()
	rec.Reset()

	data, err := rec.Export()
	if err != nil {
		t.Fatalf("Export after Reset: %v", err)
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("invalid JSON after Reset: %v", err)
	}

	logMap := doc["log"].(map[string]interface{})
	entries := logMap["entries"].([]interface{})
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after Reset, got %d", len(entries))
	}
	_ = time.Now() // satisfy import
}

func TestHARRecorder_RecordsEntry(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "hi"})

	rec := NewHARRecorder("test-tool", "1.0")
	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithHARRecorder(rec))

	_, err := c.Execute(c.Get(srv.URL() + "/ping"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if rec.EntryCount() != 1 {
		t.Errorf("expected EntryCount=1, got %d", rec.EntryCount())
	}
	entries := rec.Entries()
	if entries[0].Request.Method != http.MethodGet {
		t.Errorf("expected GET, got %q", entries[0].Request.Method)
	}
	if entries[0].Request.URL == "" {
		t.Error("entry URL should not be empty")
	}
}

func TestHARRecorder_ResponseBody(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "text/plain"},
		Body:    "hello world",
	})

	rec := NewHARRecorder()
	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithHARRecorder(rec))

	_, err := c.Execute(c.Get(srv.URL() + "/body"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entries := rec.Entries()
	if len(entries) == 0 {
		t.Fatal("no entries recorded")
	}
	if entries[0].Response.Content.Text != "hello world" {
		t.Errorf("expected body 'hello world', got %q", entries[0].Response.Content.Text)
	}
}

func TestHARRecorder_RequestHeaders(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	rec := NewHARRecorder()
	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithHARRecorder(rec))

	req := c.Get(srv.URL() + "/headers")
	req = req.WithHeader("X-Custom-Header", "relay-test")
	_, err := c.Execute(req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	entries := rec.Entries()
	if len(entries) == 0 {
		t.Fatal("no entries recorded")
	}
	var found bool
	for _, h := range entries[0].Request.Headers {
		if h.Name == "X-Custom-Header" && h.Value == "relay-test" {
			found = true
			break
		}
	}
	if !found {
		t.Error("X-Custom-Header not found in recorded request headers")
	}
}

func TestHARRecorder_ExportJSON(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "json-test"})

	rec := NewHARRecorder()
	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithHARRecorder(rec))

	_, err := c.Execute(c.Get(srv.URL() + "/export"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	data, err := rec.ExportJSON()
	if err != nil {
		t.Fatalf("ExportJSON: %v", err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	logMap, ok := doc["log"].(map[string]interface{})
	if !ok {
		t.Fatal("missing 'log' key")
	}
	if v, _ := logMap["version"].(string); v != "1.2" {
		t.Errorf("expected version '1.2', got %q", v)
	}
}

func TestHARRecorder_Reset(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	rec := NewHARRecorder()
	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithHARRecorder(rec))

	for i := 0; i < 2; i++ {
		if _, err := c.Execute(c.Get(srv.URL() + "/")); err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
	}
	if rec.EntryCount() != 2 {
		t.Fatalf("expected 2 entries before Reset, got %d", rec.EntryCount())
	}

	rec.Reset()

	if rec.EntryCount() != 0 {
		t.Errorf("expected 0 entries after Reset, got %d", rec.EntryCount())
	}
}

func TestHARRecorder_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	const n = 10
	for i := 0; i < n; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "concurrent"})
	}

	rec := NewHARRecorder()
	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithHARRecorder(rec))

	done := make(chan struct{}, n)
	for i := 0; i < n; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			if _, err := c.Execute(c.Get(srv.URL() + "/")); err != nil {
				t.Errorf("Execute error: %v", err)
			}
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}

	if rec.EntryCount() != n {
		t.Errorf("expected EntryCount=%d, got %d", n, rec.EntryCount())
	}
}
