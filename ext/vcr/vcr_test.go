package vcr

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhonsferg/relay"
)

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestRecordMode(t *testing.T) {
	// Start a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"result":"ok"}`)
	}))
	defer server.Close()

	// Create temp cassette file
	cassettePath := filepath.Join(t.TempDir(), "test.json")

	// Create VCR in record mode
	vcr, err := New(cassettePath, ModeRecord)
	if err != nil {
		t.Fatalf("Failed to create VCR: %v", err)
	}

	// Create a relay client with VCR middleware
	client := relay.New(
		relay.WithBaseURL(server.URL),
		vcr.Middleware(),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	// Make a request
	resp, err := client.Execute(client.Get("/test"))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body := resp.Body()
	if string(body) != `{"result":"ok"}` {
		t.Errorf("Unexpected response body: %s", body)
	}

	// Verify cassette was written
	if _, statErr := os.Stat(cassettePath); os.IsNotExist(statErr) {
		t.Fatal("Cassette file was not created")
	}

	// Load and verify cassette contents
	data, err := os.ReadFile(cassettePath) //nolint:gosec
	if err != nil {
		t.Fatalf("Failed to read cassette: %v", err)
	}
	var cassette Cassette
	if err := json.Unmarshal(data, &cassette); err != nil {
		t.Fatalf("Failed to unmarshal cassette: %v", err)
	}

	if len(cassette.Interactions) != 1 {
		t.Errorf("Expected 1 interaction, got %d", len(cassette.Interactions))
	}

	interaction := cassette.Interactions[0]
	if interaction.Request.Method != "GET" {
		t.Errorf("Expected GET, got %s", interaction.Request.Method)
	}
	if interaction.Response.Status != 200 {
		t.Errorf("Expected status 200, got %d", interaction.Response.Status)
	}
	if interaction.Response.Body != `{"result":"ok"}` {
		t.Errorf("Unexpected recorded body: %s", interaction.Response.Body)
	}
}

// TestRecordMode_PreservesMultiValueHeaders guards against headerMapFromHeader
// keeping only the first value for a repeated header (e.g. multiple
// Set-Cookie headers), silently dropping the rest before persisting to the
// cassette.
func TestRecordMode_PreservesMultiValueHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "a=1")
		w.Header().Add("Set-Cookie", "b=2")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cassettePath := filepath.Join(t.TempDir(), "multivalue.json")
	v, err := New(cassettePath, ModeRecord)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	client := relay.New(
		relay.WithBaseURL(server.URL),
		v.Middleware(),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)
	if _, err := client.Execute(client.Get("/test")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := v.cassette.Interactions[0].Response.Header["Set-Cookie"]
	want := []string{"a=1", "b=2"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Set-Cookie = %v, want %v", got, want)
	}
}

// TestLoadLegacyCassette_SingleValueHeaders guards backward compatibility:
// cassettes written before multi-value header support stored
// "header": {"X-Foo": "bar"} (a plain string per key) rather than the
// current {"X-Foo": ["bar"]}. Both shapes must still load.
func TestLoadLegacyCassette_SingleValueHeaders(t *testing.T) {
	cassettePath := filepath.Join(t.TempDir(), "legacy-headers.json")
	legacyJSON := `{
	  "interactions": [
	    {
	      "request": {"method": "GET", "url": "http://example.com/x", "header": {"Accept": "application/json"}, "body": ""},
	      "response": {"status": 200, "header": {"Content-Type": "application/json"}, "body": "{}"}
	    }
	  ]
	}`
	if err := os.WriteFile(cassettePath, []byte(legacyJSON), 0o600); err != nil {
		t.Fatalf("writing legacy cassette: %v", err)
	}

	v, err := New(cassettePath, ModePlayback)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	interaction := v.cassette.Interactions[0]
	if got := interaction.Request.Header["Accept"]; len(got) != 1 || got[0] != "application/json" {
		t.Errorf("Request Header[Accept] = %v, want [application/json]", got)
	}
	if got := interaction.Response.Header["Content-Type"]; len(got) != 1 || got[0] != "application/json" {
		t.Errorf("Response Header[Content-Type] = %v, want [application/json]", got)
	}
}

// TestRecordMode_ClosesRequestBody guards against a leak: the original
// req.Body is read via io.ReadAll and replaced with a NopCloser, but the
// original was never closed before this fix - http.RoundTripper's contract
// requires the body to always be closed. Uses vcrTransport directly (this
// is an in-package white-box test) so the crafted body reader reaches
// recordRoundTrip unmodified, bypassing relay's own request-building
// (which would otherwise eagerly buffer the body itself first).
func TestRecordMode_ClosesRequestBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cassettePath := filepath.Join(t.TempDir(), "close-body.json")
	v, err := New(cassettePath, ModeRecord)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	transport := &vcrTransport{vcr: v, base: http.DefaultTransport}

	body := &trackingReadCloser{Reader: strings.NewReader("payload")}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/test", body)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}
	req.ContentLength = int64(len("payload"))

	if _, err := transport.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	if !body.closed {
		t.Error("original req.Body was not closed during recording")
	}
}

// trackingReadCloser wraps an io.Reader and records whether Close was called.
type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (b *trackingReadCloser) Close() error {
	b.closed = true
	return nil
}

func TestPlaybackMode(t *testing.T) {
	cassettePath := filepath.Join(t.TempDir(), "test.json")

	// Create a cassette file with test data
	cassette := Cassette{
		Interactions: []Interaction{
			{
				Request: RecordedRequest{
					Method: "GET",
					URL:    "http://example.com/test",
					Header: map[string][]string{},
				},
				Response: RecordedResponse{
					Status: 200,
					Header: map[string][]string{"Content-Type": {"application/json"}},
					Body:   `{"result":"recorded"}`,
				},
			},
		},
	}

	data, _ := json.Marshal(cassette)
	if err := os.WriteFile(cassettePath, data, 0o600); err != nil {
		t.Fatalf("Failed to write cassette: %v", err)
	}

	// Create VCR in playback mode
	vcr, err := New(cassettePath, ModePlayback)
	if err != nil {
		t.Fatalf("Failed to create VCR: %v", err)
	}

	// Create a relay client with VCR middleware
	client := relay.New(
		relay.WithBaseURL("http://example.com"),
		vcr.Middleware(),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	// Make a request - should return recorded response without hitting server
	resp, err := client.Execute(client.Get("/test"))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body := resp.Body()
	if string(body) != `{"result":"recorded"}` {
		t.Errorf("Expected recorded response, got %s", body)
	}
}

func TestPlaybackModeExhausted(t *testing.T) {
	cassettePath := filepath.Join(t.TempDir(), "test.json")

	// Create a cassette file with one interaction
	cassette := Cassette{
		Interactions: []Interaction{
			{
				Request: RecordedRequest{
					Method: "GET",
					URL:    "http://example.com/test",
					Header: map[string][]string{},
				},
				Response: RecordedResponse{
					Status: 200,
					Header: map[string][]string{},
					Body:   `{"result":"ok"}`,
				},
			},
		},
	}

	data, _ := json.Marshal(cassette)
	if err := os.WriteFile(cassettePath, data, 0o600); err != nil {
		t.Fatalf("Failed to write cassette: %v", err)
	}

	// Create VCR in playback mode
	vcr, err := New(cassettePath, ModePlayback)
	if err != nil {
		t.Fatalf("Failed to create VCR: %v", err)
	}

	// Create a relay client with VCR middleware
	client := relay.New(
		relay.WithBaseURL("http://example.com"),
		vcr.Middleware(),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	// Make first request - should succeed
	resp1, err := client.Execute(client.Get("/test"))
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}
	_ = resp1

	// Make second request - should fail (cassette exhausted)
	_, err = client.Execute(client.Get("/test"))
	if err == nil {
		t.Fatal("Expected error when cassette exhausted, got nil")
	}
	// Check that the error is about no matching interaction
	if !contains(err.Error(), "vcr: no matching interaction found") {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestPassthroughMode(t *testing.T) {
	// Start a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"result":"passthrough"}`)
	}))
	defer server.Close()

	cassettePath := filepath.Join(t.TempDir(), "test.json")

	// Create VCR in passthrough mode
	vcr, err := New(cassettePath, ModePassthrough)
	if err != nil {
		t.Fatalf("Failed to create VCR: %v", err)
	}

	// Create a relay client with VCR middleware
	client := relay.New(
		relay.WithBaseURL(server.URL),
		vcr.Middleware(),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	// Make a request - should hit real server
	resp, err := client.Execute(client.Get("/test"))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	body := resp.Body()
	if string(body) != `{"result":"passthrough"}` {
		t.Errorf("Expected passthrough response, got %s", body)
	}

	// Verify no cassette was created
	if _, err := os.Stat(cassettePath); !os.IsNotExist(err) {
		t.Error("Cassette file was created in passthrough mode")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	cassettePath := filepath.Join(t.TempDir(), "test.json")

	// Create VCR with some interactions
	vcr, err := New(cassettePath, ModeRecord)
	if err != nil {
		t.Fatalf("Failed to create VCR: %v", err)
	}

	vcr.cassette.Interactions = []Interaction{
		{
			Request: RecordedRequest{
				Method: "POST",
				URL:    "http://api.example.com/users",
				Header: map[string][]string{"Content-Type": {"application/json"}},
				Body:   `{"name":"John"}`,
			},
			Response: RecordedResponse{
				Status: 201,
				Header: map[string][]string{"Location": {"/users/1"}},
				Body:   `{"id":1,"name":"John"}`,
			},
		},
	}

	// Save cassette
	if saveErr := vcr.Save(); saveErr != nil {
		t.Fatalf("Failed to save cassette: %v", saveErr)
	}

	// Load cassette and verify
	vcr2, err := New(cassettePath, ModePlayback)
	if err != nil {
		t.Fatalf("Failed to load cassette: %v", err)
	}

	if len(vcr2.cassette.Interactions) != 1 {
		t.Errorf("Expected 1 interaction, got %d", len(vcr2.cassette.Interactions))
	}

	interaction := vcr2.cassette.Interactions[0]
	if interaction.Request.Method != "POST" {
		t.Errorf("Expected POST, got %s", interaction.Request.Method)
	}
	if interaction.Request.Body != `{"name":"John"}` {
		t.Errorf("Unexpected request body: %s", interaction.Request.Body)
	}
	if interaction.Response.Status != 201 {
		t.Errorf("Expected status 201, got %d", interaction.Response.Status)
	}
	if interaction.Response.Body != `{"id":1,"name":"John"}` {
		t.Errorf("Unexpected response body: %s", interaction.Response.Body)
	}
}

// TestShouldSaveAfterAppend covers the debounce decision boundaries in
// isolation.
func TestShouldSaveAfterAppend(t *testing.T) {
	tests := []struct {
		n    int
		want bool
	}{
		{1, true},
		{2, true},
		{saveDebounceThreshold, true},       // 20: still within the always-save range
		{saveDebounceThreshold + 1, false},  // 21: just over threshold, not a debounce boundary
		{saveDebounceThreshold + 9, false},  // 29
		{saveDebounceThreshold + 10, true},  // 30: first debounce boundary
		{saveDebounceThreshold + 11, false}, // 31
		{saveDebounceThreshold + 20, true},  // 40: second debounce boundary
	}
	for _, tc := range tests {
		if got := shouldSaveAfterAppend(tc.n); got != tc.want {
			t.Errorf("shouldSaveAfterAppend(%d) = %v, want %v", tc.n, got, tc.want)
		}
	}
}

// TestRecordMode_LargeSessionDebouncesSavesButPreservesAllData guards
// against the O(n²) I/O regression in H5: recording a session larger than
// saveDebounceThreshold must not call saveUnlocked once per interaction, but
// once the caller flushes at the end (the already-documented pattern -
// "save the cassette when done"), every single interaction recorded along
// the way must still be present on disk, in order, with correct content.
func TestRecordMode_LargeSessionDebouncesSavesButPreservesAllData(t *testing.T) {
	const totalRequests = saveDebounceThreshold + 2*saveDebounceEvery + 3 // 43: spans multiple debounce boundaries

	var served int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served++
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"n":%d}`, served)
	}))
	defer server.Close()

	cassettePath := filepath.Join(t.TempDir(), "large.json")
	v, err := New(cassettePath, ModeRecord)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	client := relay.New(
		relay.WithBaseURL(server.URL),
		v.Middleware(),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	for i := 0; i < totalRequests; i++ {
		if _, execErr := client.Execute(client.Get("/test")); execErr != nil {
			t.Fatalf("request %d failed: %v", i, execErr)
		}
	}

	// Fewer saves than interactions proves debouncing actually happened.
	v.mu.Lock()
	saveCount := v.saveCount
	v.mu.Unlock()
	if saveCount >= totalRequests {
		t.Errorf("saveCount = %d, want fewer than %d (debouncing should reduce save frequency)", saveCount, totalRequests)
	}

	// The documented end-of-session flush must recover any interactions
	// recorded since the last debounced save.
	if saveErr := v.Save(); saveErr != nil {
		t.Fatalf("final Save: %v", saveErr)
	}

	data, err := os.ReadFile(cassettePath) //nolint:gosec
	if err != nil {
		t.Fatalf("reading cassette: %v", err)
	}
	var cassette Cassette
	if unmarshalErr := json.Unmarshal(data, &cassette); unmarshalErr != nil {
		t.Fatalf("unmarshal cassette: %v", unmarshalErr)
	}

	if len(cassette.Interactions) != totalRequests {
		t.Fatalf("cassette has %d interactions, want %d - data lost despite final Save()", len(cassette.Interactions), totalRequests)
	}
	for i, inter := range cassette.Interactions {
		want := fmt.Sprintf(`{"n":%d}`, i+1)
		if inter.Response.Body != want {
			t.Errorf("interaction %d body = %q, want %q", i, inter.Response.Body, want)
		}
	}
}

// TestBinaryBodyRoundTrip guards against H9: binary bytes (invalid UTF-8)
// in a recorded body must round-trip through save/load byte-for-byte
// instead of being corrupted by encoding/json's substitution of invalid
// UTF-8 with U+FFFD.
func TestBinaryBodyRoundTrip(t *testing.T) {
	binary := []byte{0xFF, 0xFE, 0x00, 0x01, 0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

	cassettePath := filepath.Join(t.TempDir(), "binary.json")
	v, err := New(cassettePath, ModeRecord)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	v.cassette.Interactions = []Interaction{
		{
			Request: RecordedRequest{Method: "POST", URL: "http://example.com/upload", Body: string(binary)},
			Response: RecordedResponse{
				Status: 200,
				Header: map[string][]string{"Content-Type": {"image/png"}},
				Body:   string(binary),
			},
		},
	}
	if saveErr := v.Save(); saveErr != nil {
		t.Fatalf("Save: %v", saveErr)
	}

	// The JSON on disk must be valid, and must not contain the UTF-8
	// replacement character that a plain (non-base64) encoding would emit.
	raw, err := os.ReadFile(cassettePath) //nolint:gosec
	if err != nil {
		t.Fatalf("reading cassette: %v", err)
	}
	if strings.Contains(string(raw), "�") {
		t.Error("cassette on disk contains the UTF-8 replacement character - body was corrupted before encoding")
	}
	if !strings.Contains(string(raw), `"body_encoding": "base64"`) {
		t.Errorf("expected body_encoding: base64 marker in cassette, got:\n%s", raw)
	}

	v2, err := New(cassettePath, ModePlayback)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got := v2.cassette.Interactions[0].Response.Body
	if got != string(binary) {
		t.Errorf("round-tripped body = %v, want %v", []byte(got), binary)
	}
	gotReq := v2.cassette.Interactions[0].Request.Body
	if gotReq != string(binary) {
		t.Errorf("round-tripped request body = %v, want %v", []byte(gotReq), binary)
	}
}

// TestLoadLegacyPlainTextCassette guards backward compatibility: a cassette
// written before the base64 encoding change (no body_encoding field, Body
// stored as literal text) must still load correctly.
func TestLoadLegacyPlainTextCassette(t *testing.T) {
	cassettePath := filepath.Join(t.TempDir(), "legacy.json")
	legacyJSON := `{
	  "interactions": [
	    {
	      "request": {"method": "GET", "url": "http://example.com/x", "body": ""},
	      "response": {"status": 200, "body": "{\"legacy\":true}"}
	    }
	  ]
	}`
	if err := os.WriteFile(cassettePath, []byte(legacyJSON), 0o600); err != nil {
		t.Fatalf("writing legacy cassette: %v", err)
	}

	v, err := New(cassettePath, ModePlayback)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := v.cassette.Interactions[0].Response.Body
	if got != `{"legacy":true}` {
		t.Errorf("legacy body = %q, want %q", got, `{"legacy":true}`)
	}
}

// TestNewRecordMode_CorruptCassetteReturnsError guards against H13: a
// truncated/corrupt cassette file in ModeRecord must surface an error
// instead of being silently discarded and overwritten with an empty
// cassette on the next save.
func TestNewRecordMode_CorruptCassetteReturnsError(t *testing.T) {
	cassettePath := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(cassettePath, []byte(`{"interactions": [{ this is not valid json`), 0o600); err != nil {
		t.Fatalf("writing corrupt cassette: %v", err)
	}

	_, err := New(cassettePath, ModeRecord)
	if err == nil {
		t.Fatal("expected New(ModeRecord) to return an error for a corrupt cassette file, got nil")
	}
}

// TestNewRecordMode_MissingCassetteIsNotAnError confirms the fix for H13
// didn't overcorrect: a cassette file that simply doesn't exist yet is a
// normal, expected state for ModeRecord (first recording), not an error.
func TestNewRecordMode_MissingCassetteIsNotAnError(t *testing.T) {
	cassettePath := filepath.Join(t.TempDir(), "does-not-exist.json")
	v, err := New(cassettePath, ModeRecord)
	if err != nil {
		t.Fatalf("New(ModeRecord) with a missing cassette file: %v", err)
	}
	if len(v.cassette.Interactions) != 0 {
		t.Errorf("expected an empty cassette, got %d interactions", len(v.cassette.Interactions))
	}
}

func TestMethodAndURLMatching(t *testing.T) {
	cassettePath := filepath.Join(t.TempDir(), "test.json")

	// Create a cassette file with multiple interactions
	cassette := Cassette{
		Interactions: []Interaction{
			{
				Request: RecordedRequest{
					Method: "GET",
					URL:    "http://example.com/users/1",
				},
				Response: RecordedResponse{
					Status: 200,
					Body:   `{"id":1,"name":"Alice"}`,
				},
			},
			{
				Request: RecordedRequest{
					Method: "POST",
					URL:    "http://example.com/users",
				},
				Response: RecordedResponse{
					Status: 201,
					Body:   `{"id":2}`,
				},
			},
			{
				Request: RecordedRequest{
					Method: "GET",
					URL:    "http://example.com/users/2",
				},
				Response: RecordedResponse{
					Status: 200,
					Body:   `{"id":2,"name":"Bob"}`,
				},
			},
		},
	}

	data, _ := json.Marshal(cassette)
	if err := os.WriteFile(cassettePath, data, 0o600); err != nil {
		t.Fatalf("Failed to write cassette: %v", err)
	}

	vcr, err := New(cassettePath, ModePlayback)
	if err != nil {
		t.Fatalf("Failed to create VCR: %v", err)
	}

	client := relay.New(
		relay.WithBaseURL("http://example.com"),
		vcr.Middleware(),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	// Request GET /users/1
	resp1, err := client.Execute(client.Get("/users/1"))
	if err != nil {
		t.Fatalf("Request 1 failed: %v", err)
	}
	body1 := resp1.Body()
	if string(body1) != `{"id":1,"name":"Alice"}` {
		t.Errorf("Request 1 got wrong response: %s", body1)
	}

	// Request POST /users
	resp2, err := client.Execute(client.Post("/users"))
	if err != nil {
		t.Fatalf("Request 2 failed: %v", err)
	}
	body2 := resp2.Body()
	if string(body2) != `{"id":2}` {
		t.Errorf("Request 2 got wrong response: %s", body2)
	}

	// Request GET /users/2
	resp3, err := client.Execute(client.Get("/users/2"))
	if err != nil {
		t.Fatalf("Request 3 failed: %v", err)
	}
	body3 := resp3.Body()
	if string(body3) != `{"id":2,"name":"Bob"}` {
		t.Errorf("Request 3 got wrong response: %s", body3)
	}
}
