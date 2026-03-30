// Package main demonstrates relay's ExecuteSSE helper for consuming
// Server-Sent Events (SSE) streams. The example spins up an in-process SSE
// server that emits five events and shows how to parse them with type-safe
// field access, stop early, and handle errors.
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	relay "github.com/jhonsferg/relay"
)

func main() {
	// -------------------------------------------------------------------------
	// 1. Spin up an in-process SSE server.
	//
	// A real server would stream events as they become available (database
	// changes, log tails, live metrics, …). Here we emit five numbered events
	// and close the connection.
	// -------------------------------------------------------------------------
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		events := []struct {
			id    int
			event string
			data  string
		}{
			{1, "status", `{"level":"info","msg":"startup complete"}`},
			{2, "metric", `{"cpu":12.4,"mem":68.1}`},
			{3, "metric", `{"cpu":15.2,"mem":70.3}`},
			{4, "status", `{"level":"warn","msg":"high memory usage"}`},
			{5, "close", `{"reason":"stream finished"}`},
		}

		for _, ev := range events {
			fmt.Fprintf(w, "id: %d\n", ev.id)
			fmt.Fprintf(w, "event: %s\n", ev.event)
			fmt.Fprintf(w, "data: %s\n\n", ev.data)
			flusher.Flush()
			time.Sleep(50 * time.Millisecond) // simulate real-time pacing
		}
	}))
	defer srv.Close()

	fmt.Println("=== relay SSE example ===")

	// -------------------------------------------------------------------------
	// 2. Consume the stream with ExecuteSSE.
	//
	// The SSEHandler receives one relay.SSEEvent per dispatched event. Return
	// true to continue reading, false to stop early and close the connection.
	// -------------------------------------------------------------------------
	client := relay.New()

	received := 0
	err := client.ExecuteSSE(
		client.Get(srv.URL).
			WithHeader("Accept", "text/event-stream"),
		func(ev relay.SSEEvent) bool {
			received++
			fmt.Printf("[event #%s] type=%-8s data=%s\n", ev.ID, ev.Event, ev.Data)

			// Stop consuming after the "close" event.
			return ev.Event != "close"
		},
	)
	if err != nil {
		log.Fatalf("SSE stream error: %v", err)
	}

	fmt.Printf("\nReceived %d events.\n", received)

	// -------------------------------------------------------------------------
	// 3. Multi-line data fields.
	//
	// Each "data:" line is concatenated with a newline separator per the spec.
	// -------------------------------------------------------------------------
	multiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Emit one event with three data lines.
		fmt.Fprint(w, "event: poem\n")
		fmt.Fprint(w, "data: Roses are red\n")
		fmt.Fprint(w, "data: Violets are blue\n")
		fmt.Fprint(w, "data: relay is fast\n")
		fmt.Fprint(w, "\n") // blank line = dispatch event
	}))
	defer multiSrv.Close()

	fmt.Println("\n=== multi-line data ===")
	client.ExecuteSSE( //nolint:errcheck
		client.Get(multiSrv.URL),
		func(ev relay.SSEEvent) bool {
			fmt.Printf("event=%s\ndata:\n%s\n", ev.Event, ev.Data)
			return false
		},
	)

	// -------------------------------------------------------------------------
	// 4. Per-request timeout with SSE.
	//
	// ExecuteSSE wraps ExecuteStream, so Request.WithTimeout cancels the stream
	// if it doesn't begin within the deadline.
	// -------------------------------------------------------------------------
	fmt.Println("\n=== per-request timeout ===")
	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		time.Sleep(5 * time.Second) // never responds in time
	}))
	defer slowSrv.Close()

	err = client.ExecuteSSE(
		client.Get(slowSrv.URL).WithTimeout(100*time.Millisecond),
		func(ev relay.SSEEvent) bool { return true },
	)
	if err != nil {
		fmt.Printf("expected timeout error: %v\n", err)
	}
}
