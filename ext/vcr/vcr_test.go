package vcr

import (
	"encoding/json"
	"fmt"
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

func TestPlaybackMode(t *testing.T) {
	cassettePath := filepath.Join(t.TempDir(), "test.json")

	// Create a cassette file with test data
	cassette := Cassette{
		Interactions: []Interaction{
			{
				Request: RecordedRequest{
					Method: "GET",
					URL:    "http://example.com/test",
					Header: map[string]string{},
				},
				Response: RecordedResponse{
					Status: 200,
					Header: map[string]string{"Content-Type": "application/json"},
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
					Header: map[string]string{},
				},
				Response: RecordedResponse{
					Status: 200,
					Header: map[string]string{},
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
				Header: map[string]string{"Content-Type": "application/json"},
				Body:   `{"name":"John"}`,
			},
			Response: RecordedResponse{
				Status: 201,
				Header: map[string]string{"Location": "/users/1"},
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
