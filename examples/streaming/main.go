// Package main demonstrates relay's ExecuteStream for large or streaming
// responses. The example simulates an SSE (Server-Sent Events) feed: it reads
// the response body line by line without buffering the entire payload in memory.
//
// Key rule: always close StreamResponse.Body - forgetting it leaks the TCP
// connection and an open goroutine.
package main

import (
	"bufio"
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
	// Test server: writes an SSE-style stream of 10 events, one per line, then
	// closes the connection. A real SSE server keeps the connection open.
	// ---------------------------------------------------------------------------
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Transfer-Encoding", "chunked")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		for i := 1; i <= 10; i++ {
			fmt.Fprintf(w, "data: {\"seq\":%d,\"msg\":\"event number %d\"}\n\n", i, i)
			flusher.Flush()
			time.Sleep(10 * time.Millisecond) // simulate real event cadence
		}
	}))
	defer srv.Close()

	// ---------------------------------------------------------------------------
	// Build a client. Streaming bypasses the retry loop because a partially
	// consumed body cannot be rewound.
	// ---------------------------------------------------------------------------
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithTimeout(30*time.Second),
	)

	// ---------------------------------------------------------------------------
	// ExecuteStream returns a *StreamResponse with a live Body reader.
	// The body is NOT buffered - data arrives as the server writes it.
	// ---------------------------------------------------------------------------
	stream, err := client.ExecuteStream(
		client.Get("/events").
			WithHeader("Accept", "text/event-stream"),
	)
	if err != nil {
		log.Fatalf("ExecuteStream failed: %v", err)
	}

	// ALWAYS close the body, even on error paths below. Use defer so it is
	// called regardless of how the function exits. Closing releases the
	// underlying TCP connection back to the pool.
	defer stream.Body.Close() //nolint:errcheck

	// Check the status code before consuming the body.
	if stream.IsError() {
		log.Fatalf("server returned error: %s", stream.Status)
	}
	fmt.Printf("Connected to stream - status: %d %s\n", stream.StatusCode, stream.Status)
	fmt.Printf("Content-Type: %s\n\n", stream.ContentType())

	// ---------------------------------------------------------------------------
	// Read line by line using bufio.Scanner.
	//
	// SSE lines look like:  data: <json payload>\n\n
	// We skip blank separator lines and strip the "data: " prefix.
	// ---------------------------------------------------------------------------
	eventCount := 0
	scanner := bufio.NewScanner(stream.Body)
	for scanner.Scan() {
		line := scanner.Text()

		// SSE blank lines separate events - skip them.
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Strip the "data: " prefix that SSE mandates.
		payload := strings.TrimPrefix(line, "data: ")
		eventCount++
		fmt.Printf("event %2d: %s\n", eventCount, payload)
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("error reading stream: %v", err)
	}

	fmt.Printf("\nStream complete - received %d events\n", eventCount)

	// ---------------------------------------------------------------------------
	// Download-progress variant
	//
	// For large binary downloads (not SSE), attach a ProgressFunc via
	// WithDownloadProgress on the request. ExecuteStream respects this when the
	// server sends a Content-Length header.
	// ---------------------------------------------------------------------------
	fmt.Println("\n=== Download with progress callback ===")

	binarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := strings.Repeat("A", 1024*8) // 8 KB
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		fmt.Fprint(w, payload)
	}))
	defer binarySrv.Close()

	binaryClient := relay.New(relay.WithBaseURL(binarySrv.URL))
	var lastPct int
	stream2, err := binaryClient.ExecuteStream(
		binaryClient.Get("/binary").
			WithDownloadProgress(func(transferred, total int64) {
				if total > 0 {
					pct := int(transferred * 100 / total)
					if pct != lastPct {
						fmt.Printf("  download progress: %d / %d bytes (%d%%)\n", transferred, total, pct)
						lastPct = pct
					}
				}
			}),
	)
	if err != nil {
		log.Fatalf("binary stream failed: %v", err)
	}
	defer stream2.Body.Close() //nolint:errcheck

	// Drain the body so the progress callback fires.
	scanner2 := bufio.NewScanner(stream2.Body)
	var totalBytes int
	for scanner2.Scan() {
		totalBytes += len(scanner2.Bytes())
	}
	fmt.Printf("  downloaded %d bytes total\n", totalBytes)
}
