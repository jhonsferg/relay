// Package main demonstrates relay.ExecuteAsStream[T] for consuming
// newline-delimited JSON (JSONL / NDJSON) streams. This pattern is common in
// AI APIs (OpenAI, Anthropic), log aggregation services, and event pipelines
// that produce a continuous stream of JSON objects.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"

	relay "github.com/jhonsferg/relay"
)

// LogLine represents a single structured log entry in the JSONL stream.
type LogLine struct {
	Level   string `json:"level"`
	Message string `json:"message"`
	Service string `json:"service"`
	Code    int    `json:"code,omitempty"`
}

// ChatDelta is the shape of one chunk in an OpenAI-style streaming response.
type ChatDelta struct {
	ID      string `json:"id"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

func main() {
	// -------------------------------------------------------------------------
	// 1. JSONL log stream - read all records.
	// -------------------------------------------------------------------------
	logs := []LogLine{
		{Level: "info", Message: "server started", Service: "api", Code: 0},
		{Level: "info", Message: "request received", Service: "api"},
		{Level: "warn", Message: "slow database query", Service: "db", Code: 503},
		{Level: "error", Message: "connection timeout", Service: "cache", Code: 408},
		{Level: "info", Message: "request completed", Service: "api"},
	}

	logSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		enc := json.NewEncoder(w)
		for _, l := range logs {
			enc.Encode(l) //nolint:errcheck
		}
	}))
	defer logSrv.Close()

	client := relay.New()

	fmt.Println("=== JSONL log stream (all records) ===")
	err := relay.ExecuteAsStream[LogLine](
		client,
		client.Get(logSrv.URL),
		func(line LogLine) bool {
			icon := "✓"
			if line.Level == "warn" {
				icon = "⚠"
			} else if line.Level == "error" {
				icon = "✗"
			}
			fmt.Printf("  %s [%s] %-8s %s", icon, line.Level, line.Service, line.Message)
			if line.Code != 0 {
				fmt.Printf(" (code=%d)", line.Code)
			}
			fmt.Println()
			return true // keep reading
		},
	)
	if err != nil {
		log.Fatalf("stream error: %v", err)
	}

	// -------------------------------------------------------------------------
	// 2. Stop early - read only until first error line.
	// -------------------------------------------------------------------------
	fmt.Println("\n=== stop on first error ===")
	count := 0
	relay.ExecuteAsStream[LogLine]( //nolint:errcheck
		client,
		client.Get(logSrv.URL),
		func(line LogLine) bool {
			count++
			fmt.Printf("  [%d] %s: %s\n", count, line.Level, line.Message)
			return line.Level != "error" // stop when we see an error
		},
	)
	fmt.Printf("  stopped after %d records\n", count)

	// -------------------------------------------------------------------------
	// 3. OpenAI-style streaming chat completion.
	//
	// Each chunk has an "id" and a list of "choices" containing a delta with
	// the partial content. We concatenate the content pieces into a full reply.
	// -------------------------------------------------------------------------
	words := []string{"Hello", ",", " ", "world", "!", " ", "Streaming", " works", "."}
	chatSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		enc := json.NewEncoder(w)
		for i, word := range words {
			delta := ChatDelta{
				ID: fmt.Sprintf("chatcmpl-%04d", i),
				Choices: []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					FinishReason *string `json:"finish_reason"`
				}{
					{Delta: struct {
						Content string `json:"content"`
					}{Content: word}},
				},
			}
			enc.Encode(delta) //nolint:errcheck
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		stop := "stop"
		final := ChatDelta{
			ID: "chatcmpl-final",
			Choices: []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			}{
				{FinishReason: &stop},
			},
		}
		enc.Encode(final) //nolint:errcheck
	}))
	defer chatSrv.Close()

	fmt.Println("\n=== OpenAI-style streaming chat ===")
	var fullReply string
	relay.ExecuteAsStream[ChatDelta]( //nolint:errcheck
		client,
		client.Get(chatSrv.URL),
		func(chunk ChatDelta) bool {
			for _, choice := range chunk.Choices {
				if choice.FinishReason != nil {
					fmt.Printf("\n  [finish_reason=%s]\n", *choice.FinishReason)
					return false
				}
				fullReply += choice.Delta.Content
				fmt.Print(choice.Delta.Content)
			}
			return true
		},
	)
	fmt.Printf("\n  full reply: %q\n", fullReply)

	// -------------------------------------------------------------------------
	// 4. Clone a base request for reuse across multiple stream calls.
	// -------------------------------------------------------------------------
	fmt.Println("\n=== request Clone() for reuse ===")
	base := client.Get(logSrv.URL).
		WithHeader("Accept", "application/x-ndjson").
		WithTag("op", "log-stream")

	for i, level := range []string{"info", "error"} {
		target := level // capture for closure
		req := base.Clone().WithQueryParam("level", target)
		count := 0
		relay.ExecuteAsStream[LogLine]( //nolint:errcheck
			client, req,
			func(l LogLine) bool {
				count++
				return true
			},
		)
		fmt.Printf("  clone %d (level=%s): read %d lines\n", i+1, target, count)
	}
}
