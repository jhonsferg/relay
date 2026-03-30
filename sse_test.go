package relay_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhonsferg/relay"
)

func sseServer(events string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		_, _ = fmt.Fprint(w, events)
	}))
}

func TestExecuteSSE_BasicEvent(t *testing.T) {
	srv := sseServer("data: hello\n\n")
	defer srv.Close()

	client := relay.New()
	var received []relay.SSEEvent
	err := client.ExecuteSSE(client.Get(srv.URL), func(ev relay.SSEEvent) bool {
		received = append(received, ev)
		return true
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].Data != "hello" {
		t.Errorf("data = %q, want %q", received[0].Data, "hello")
	}
	if received[0].Event != "message" {
		t.Errorf("event type = %q, want %q", received[0].Event, "message")
	}
}

func TestExecuteSSE_AllFields(t *testing.T) {
	raw := "id: 42\nevent: update\ndata: payload\nretry: 3000\n\n"
	srv := sseServer(raw)
	defer srv.Close()

	client := relay.New()
	var got relay.SSEEvent
	err := client.ExecuteSSE(client.Get(srv.URL), func(ev relay.SSEEvent) bool {
		got = ev
		return false
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "42" {
		t.Errorf("ID = %q, want %q", got.ID, "42")
	}
	if got.Event != "update" {
		t.Errorf("Event = %q, want %q", got.Event, "update")
	}
	if got.Data != "payload" {
		t.Errorf("Data = %q, want %q", got.Data, "payload")
	}
	if got.Retry != 3000 {
		t.Errorf("Retry = %d, want 3000", got.Retry)
	}
}

func TestExecuteSSE_MultiLineData(t *testing.T) {
	raw := "data: line1\ndata: line2\ndata: line3\n\n"
	srv := sseServer(raw)
	defer srv.Close()

	client := relay.New()
	var got relay.SSEEvent
	err := client.ExecuteSSE(client.Get(srv.URL), func(ev relay.SSEEvent) bool {
		got = ev
		return false
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "line1\nline2\nline3"
	if got.Data != want {
		t.Errorf("Data = %q, want %q", got.Data, want)
	}
}

func TestExecuteSSE_MultipleEvents(t *testing.T) {
	raw := "data: first\n\ndata: second\n\ndata: third\n\n"
	srv := sseServer(raw)
	defer srv.Close()

	client := relay.New()
	var events []relay.SSEEvent
	err := client.ExecuteSSE(client.Get(srv.URL), func(ev relay.SSEEvent) bool {
		events = append(events, ev)
		return true
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	for i, want := range []string{"first", "second", "third"} {
		if events[i].Data != want {
			t.Errorf("event[%d].Data = %q, want %q", i, events[i].Data, want)
		}
	}
}

func TestExecuteSSE_StopEarly(t *testing.T) {
	raw := "data: one\n\ndata: two\n\ndata: three\n\n"
	srv := sseServer(raw)
	defer srv.Close()

	client := relay.New()
	count := 0
	_ = client.ExecuteSSE(client.Get(srv.URL), func(ev relay.SSEEvent) bool {
		count++
		return count < 2 // stop after second event
	})
	if count != 2 {
		t.Errorf("handler called %d times, want 2", count)
	}
}

func TestExecuteSSE_CommentsIgnored(t *testing.T) {
	raw := ": this is a comment\ndata: real\n\n"
	srv := sseServer(raw)
	defer srv.Close()

	client := relay.New()
	var got relay.SSEEvent
	err := client.ExecuteSSE(client.Get(srv.URL), func(ev relay.SSEEvent) bool {
		got = ev
		return false
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Data != "real" {
		t.Errorf("Data = %q, want %q", got.Data, "real")
	}
}

func TestExecuteSSE_HandlerReturnFalseOnFirst(t *testing.T) {
	raw := "data: only\n\ndata: never\n\n"
	srv := sseServer(raw)
	defer srv.Close()

	client := relay.New()
	calls := 0
	err := client.ExecuteSSE(client.Get(srv.URL), func(ev relay.SSEEvent) bool {
		calls++
		return false
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("handler called %d times, want 1", calls)
	}
}
