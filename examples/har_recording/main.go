// Package main demonstrates relay's HAR 1.2 traffic recorder via
// relay.WithHARRecording. HAR (HTTP Archive) is a JSON format supported by
// browser DevTools, Charles Proxy, Postman, and many API-testing tools.
//
// Use cases:
//   - Capture outgoing API traffic for debugging or replay
//   - Generate test fixtures from real responses
//   - Audit all requests made by a client in an integration test
//   - Share reproducible bug reports including full request/response detail
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	relay "github.com/jhonsferg/relay"
)

func main() {
	// ---------------------------------------------------------------------------
	// 1. Test server: realistic responses with varied headers and bodies.
	// ---------------------------------------------------------------------------
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "req-001")

		switch r.URL.Path {
		case "/users/1":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":1,"name":"Alice","role":"admin"}`)
		case "/users/1/posts":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[{"id":10,"title":"Hello"},{"id":11,"title":"World"}]`)
		case "/users":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":42,"name":"Bob"}`)
		case "/forbidden":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"error":"access denied"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// ---------------------------------------------------------------------------
	// 2. Create a HARRecorder and attach it to the client.
	//
	// relay.WithHARRecording wraps the transport so every request/response pair
	// is captured in memory. Call recorder.Entries() for structured access, or
	// recorder.Export() for the full HAR JSON document.
	// ---------------------------------------------------------------------------
	recorder := relay.NewHARRecorder()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithHARRecording(recorder),
		relay.WithTimeout(10*time.Second),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithDefaultHeaders(map[string]string{
			"Accept":     "application/json",
			"User-Agent": "relay-har-example/1.0",
		}),
	)

	// ---------------------------------------------------------------------------
	// 3. Execute a realistic sequence of API calls.
	// ---------------------------------------------------------------------------
	fmt.Println("Executing requests…")

	resp, err := client.Execute(client.Get("/users/1"))
	if err != nil {
		log.Fatalf("GET /users/1: %v", err)
	}
	fmt.Printf("  GET  /users/1        → %d\n", resp.StatusCode)

	resp, err = client.Execute(client.Get("/users/1/posts"))
	if err != nil {
		log.Fatalf("GET /users/1/posts: %v", err)
	}
	fmt.Printf("  GET  /users/1/posts  → %d\n", resp.StatusCode)

	resp, err = client.Execute(client.Post("/users").
		WithJSON(map[string]string{"name": "Bob", "role": "viewer"}))
	if err != nil {
		log.Fatalf("POST /users: %v", err)
	}
	fmt.Printf("  POST /users          → %d\n", resp.StatusCode)

	resp, err = client.Execute(client.Get("/forbidden"))
	if err != nil {
		log.Fatalf("GET /forbidden: %v", err)
	}
	fmt.Printf("  GET  /forbidden      → %d\n\n", resp.StatusCode)

	// ---------------------------------------------------------------------------
	// 4. Inspect entries programmatically via Entries().
	//
	// Each HAREntry has a .Request (method, url, headers, body) and .Response
	// (status, headers, body text) plus per-entry timings.
	// ---------------------------------------------------------------------------
	entries := recorder.Entries()
	fmt.Printf("Captured %d entries:\n\n", len(entries))

	fmt.Println("--- Entry summary ---")
	for i, e := range entries {
		fmt.Printf("[%d] %s %s\n", i+1, e.Request.Method, e.Request.URL)
		fmt.Printf("    Status  : %d %s\n", e.Response.Status, e.Response.StatusText)
		fmt.Printf("    Timings : send=%.2fms wait=%.2fms receive=%.2fms total=%.2fms\n",
			e.Timings.Send, e.Timings.Wait, e.Timings.Receive, e.Time)
		fmt.Printf("    MimeType: %s\n", e.Response.Content.MimeType)
		if body := e.Response.Content.Text; body != "" {
			if len(body) > 60 {
				body = body[:60] + "…"
			}
			fmt.Printf("    Body    : %s\n", body)
		}
		// Print request headers.
		for _, h := range e.Request.Headers {
			if h.Name == "User-Agent" || h.Name == "Accept" {
				fmt.Printf("    Header  : %s: %s\n", h.Name, h.Value)
			}
		}
		fmt.Println()
	}

	// ---------------------------------------------------------------------------
	// 5. Export as HAR JSON and print a preview.
	//
	// In production: write the bytes to a .har file, attach to a bug report, or
	// import into a tool like Insomnia / Postman / browser DevTools.
	// ---------------------------------------------------------------------------
	raw, err := recorder.Export()
	if err != nil {
		log.Fatalf("Export: %v", err)
	}

	fmt.Printf("--- HAR JSON (%d bytes total, first 500 shown) ---\n", len(raw))
	preview := string(raw)
	if len(preview) > 500 {
		cut := strings.LastIndex(preview[:500], "\n")
		if cut < 0 {
			cut = 500
		}
		preview = preview[:cut] + "\n  …"
	}
	fmt.Println(preview)

	// ---------------------------------------------------------------------------
	// 6. Reset and re-use the recorder.
	//
	// Useful in test suites where you want a clean slate between test cases
	// without creating a new client.
	// ---------------------------------------------------------------------------
	fmt.Printf("\n--- After Reset ---\n")
	recorder.Reset()
	fmt.Printf("Entries after Reset(): %d\n", len(recorder.Entries()))

	// Fire one more request to confirm recording resumes.
	client.Execute(client.Get("/users/1")) //nolint:errcheck
	fmt.Printf("Entries after one more request: %d\n", len(recorder.Entries()))

	// ---------------------------------------------------------------------------
	// 7. Multiple clients writing to the same recorder.
	//
	// HARRecorder is safe for concurrent use, so multiple clients (e.g., one
	// per upstream service) can funnel all traffic into a single archive.
	// ---------------------------------------------------------------------------
	fmt.Println("\n--- Shared recorder across two clients ---")
	sharedRecorder := relay.NewHARRecorder()

	clientA := relay.New(relay.WithBaseURL(srv.URL), relay.WithHARRecording(sharedRecorder), relay.WithDisableRetry())
	clientB := relay.New(relay.WithBaseURL(srv.URL), relay.WithHARRecording(sharedRecorder), relay.WithDisableRetry())

	clientA.Execute(clientA.Get("/users/1"))   //nolint:errcheck
	clientB.Execute(clientB.Get("/forbidden")) //nolint:errcheck

	fmt.Printf("Shared recorder entries: %d (from two different clients)\n", len(sharedRecorder.Entries()))
}
