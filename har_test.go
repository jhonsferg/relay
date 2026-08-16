package relay

import (
	"encoding/json"
	"errors"
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

// TestHARRecorder_LargeBodySentInFullNotTruncated guards against
// maxHARBodySize (10 MB, meant to bound only the *recorded* HAR text
// snapshot) also capping what io.ReadAll read from req.Body before it gets
// reassigned - since that reassigned body is what actually goes out over
// the network, a request body larger than 10 MB was previously silently
// truncated to 10 MB before ever reaching the server whenever HAR
// recording was enabled.
func TestHARRecorder_LargeBodySentInFullNotTruncated(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	const bodySize = 11 * 1024 * 1024 // > maxHARBodySize (10 MB)
	body := make([]byte, bodySize)
	for i := range body {
		body[i] = byte(i % 256)
	}

	rec := NewHARRecorder()
	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithHARRecording(rec))
	_, err := c.Execute(c.Post(srv.URL() + "/upload").WithBody(body))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, err := srv.TakeRequest(time.Second)
	if err != nil {
		t.Fatalf("TakeRequest: %v", err)
	}
	if len(got.Body) != bodySize {
		t.Errorf("server received %d bytes, want %d (full untruncated body) - HAR recording truncated the actual request", len(got.Body), bodySize)
	}

	entries := rec.Entries()
	if len(entries) == 0 {
		t.Fatal("expected at least 1 HAR entry")
	}
	entry := entries[0]
	if entry.Request.BodySize != bodySize {
		t.Errorf("HAR entry BodySize = %d, want %d (the true full size)", entry.Request.BodySize, bodySize)
	}
	if len(entry.Request.PostData.Text) > 10*1024*1024 {
		t.Errorf("HAR recorded postData.Text = %d bytes, want capped at 10MB (the recording, not the sent body, should be bounded)", len(entry.Request.PostData.Text))
	}
}

// TestHARRecorder_LargeResponseBodyDeliveredInFullNotTruncated is the
// response-side counterpart to TestHARRecorder_LargeBodySentInFullNotTruncated
// above. harTransport.RoundTrip reads resp.Body through
// io.LimitReader(resp.Body, maxHARRespBodySize) and then reassigns
// resp.Body = io.NopCloser(newBytesReader(body)) using that same
// (possibly-truncated-at-10MB) byte slice for BOTH the HAR recording AND
// the response actually returned to the caller. Unlike the request-body fix,
// which reads unbounded and only caps what's stored in postData.Text, the
// response side truncates the real, returned Response.Body() to 10 MB
// whenever HAR recording is enabled and the server sends a larger body.
func TestHARRecorder_LargeResponseBodyDeliveredInFullNotTruncated(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	const bodySize = 11 * 1024 * 1024 // > maxHARRespBodySize (10 MB)
	body := make([]byte, bodySize)
	for i := range body {
		body[i] = byte(i % 256)
	}
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: string(body)})

	rec := NewHARRecorder()
	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithHARRecording(rec), WithMaxResponseBodyBytes(0))
	resp, err := c.Execute(c.Get(srv.URL() + "/large"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer PutResponse(resp)

	if got := len(resp.Body()); got != bodySize {
		t.Errorf("caller received %d bytes, want %d (full untruncated response) - HAR recording truncated the actual response", got, bodySize)
	}

	entries := rec.Entries()
	if len(entries) == 0 {
		t.Fatal("expected at least 1 HAR entry")
	}
	if got := len(entries[0].Response.Content.Text); got > 10*1024*1024 {
		t.Errorf("HAR recorded response Content.Text = %d bytes, want capped at 10MB (the recording, not the delivered body, should be bounded)", got)
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

// TestHARRecorder_Middleware guards the public Middleware() wrapper, which
// WithHARRecording bypasses (it wires newHARTransport directly) - meaning
// Middleware is the only path exercised when a caller composes it manually
// via WithTransportMiddleware, e.g. to place HAR recording at a specific
// point in a custom transport chain.
func TestHARRecorder_Middleware(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: `{"via":"middleware"}`})

	rec := NewHARRecorder()
	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithTransportMiddleware(rec.Middleware()),
	)

	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if rec.EntryCount() != 1 {
		t.Errorf("expected 1 recorded entry via Middleware(), got %d", rec.EntryCount())
	}
}

// TestHARRecorder_ExportHAR guards the struct-returning export path (as
// opposed to Export/ExportJSON, which serialise to bytes).
func TestHARRecorder_ExportHAR(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusCreated, Body: `{"id":1}`})

	rec := NewHARRecorder("mytool", "9.9")
	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithHARRecording(rec))

	if _, err := c.Execute(c.Post(srv.URL() + "/create").WithBody([]byte(`{"x":1}`))); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	doc := rec.ExportHAR()
	if doc == nil {
		t.Fatal("ExportHAR returned nil")
	}
	if doc.Log.Version != "1.2" {
		t.Errorf("Log.Version = %q, want 1.2", doc.Log.Version)
	}
	if doc.Log.Creator.Name != "mytool" || doc.Log.Creator.Version != "9.9" {
		t.Errorf("Creator = %+v, want {mytool 9.9}", doc.Log.Creator)
	}
	if len(doc.Log.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(doc.Log.Entries))
	}
	if doc.Log.Entries[0].Response.Status != http.StatusCreated {
		t.Errorf("entry status = %d, want 201", doc.Log.Entries[0].Response.Status)
	}

	// Mutating the returned document must not affect the recorder's
	// internal state - ExportHAR copies entries, it doesn't alias them.
	doc.Log.Entries[0].Response.Status = 999
	doc2 := rec.ExportHAR()
	if doc2.Log.Entries[0].Response.Status != http.StatusCreated {
		t.Errorf("ExportHAR result was aliased: second call reflects mutation from the first")
	}
}

// TestHARRecorder_All guards the iter.Seq iterator, including early
// termination when the yield function returns false.
func TestHARRecorder_All(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	for i := 0; i < 3; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "entry"})
	}

	rec := NewHARRecorder()
	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithHARRecording(rec))

	for i := 0; i < 3; i++ {
		if _, err := c.Execute(c.Get(srv.URL() + "/")); err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
	}

	var collected []HAREntry
	for entry := range rec.All() {
		collected = append(collected, entry)
	}
	if len(collected) != 3 {
		t.Fatalf("expected 3 entries from All(), got %d", len(collected))
	}

	// Early termination: stop after the first entry.
	var count int
	for range rec.All() {
		count++
		break
	}
	if count != 1 {
		t.Errorf("expected iteration to stop after 1 entry, got %d", count)
	}
}

// TestHARTransport_RoundTrip_TransportError guards the branch where the
// underlying transport itself fails (as opposed to a body-read failure) -
// no entry should be recorded, and the error must propagate unchanged.
func TestHARTransport_RoundTrip_TransportError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("simulated transport failure")
	rec := NewHARRecorder()
	transport := newHARTransport(roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, wantErr
	}), rec)

	req, _ := http.NewRequest(http.MethodGet, "http://example.invalid/", nil)
	_, err := transport.RoundTrip(req)
	if !errors.Is(err, wantErr) {
		t.Errorf("expected the underlying transport error to propagate unchanged, got: %v", err)
	}
	if rec.EntryCount() != 0 {
		t.Errorf("expected no entry recorded on transport error, got %d", rec.EntryCount())
	}
}

// TestBuildHARRequest_NoBody guards the request-with-no-body branch (GET
// requests, or a body that reads as empty) - PostData must stay nil rather
// than being set to an empty string.
func TestBuildHARRequest_NoBody(t *testing.T) {
	t.Parallel()
	req, _ := http.NewRequest(http.MethodGet, "http://example.com/x?a=1", nil)
	harReq := buildHARRequest(req)
	if harReq.PostData != nil {
		t.Errorf("expected nil PostData for a bodyless request, got %+v", harReq.PostData)
	}
	if harReq.Method != http.MethodGet {
		t.Errorf("Method = %q, want GET", harReq.Method)
	}
	if len(harReq.QueryString) != 1 || harReq.QueryString[0].Name != "a" {
		t.Errorf("QueryString = %+v, want [{a 1}]", harReq.QueryString)
	}
}
