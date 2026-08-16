package relay_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

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

func TestExecuteSSEWithReconnect_SingleStream(t *testing.T) {
	raw := "id: 1\nevent: update\ndata: hello\n\n"
	srv := sseServer(raw)
	defer srv.Close()

	client := relay.New()
	var received []relay.SSEEvent
	cfg := relay.SSEClientConfig{
		MaxReconnects:  0,
		ReconnectDelay: 1 * time.Millisecond,
	}
	err := client.ExecuteSSEWithReconnect(
		client.Get(srv.URL),
		cfg,
		func(ev relay.SSEEvent) bool {
			received = append(received, ev)
			return false
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	if received[0].Data != "hello" {
		t.Errorf("data = %q, want %q", received[0].Data, "hello")
	}
}

// TestExecuteSSEWithReconnect_DoesNotMutateOriginalRequest guards against a
// regression where each reconnect attempt was built via a shallow struct
// copy (`attempt := *req`) instead of req.Clone(). Since Request.headers is
// a map, a shallow copy shares the same underlying map with the original
// req - so writing the Last-Event-ID header for a reconnect attempt (via
// WithHeader, which mutates in place) leaked that header back into the
// caller's original req whenever it already had at least one header set,
// permanently corrupting it for any later reuse after this call returns.
func TestExecuteSSEWithReconnect_DoesNotMutateOriginalRequest(t *testing.T) {
	var mu sync.Mutex
	var seenHeaders []http.Header
	var connCount int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenHeaders = append(seenHeaders, r.Header.Clone())
		connCount++
		n := connCount
		mu.Unlock()

		if n <= 2 {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Connection", "keep-alive")
			_, _ = fmt.Fprint(w, "id: 1\ndata: hello\n\n")
			return
		}
		// Third connection: the plain request reusing the original req
		// after ExecuteSSEWithReconnect has returned.
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New()
	// A pre-existing header is required to reproduce the bug: it forces
	// req.headers to be a non-nil map that a shallow copy would share.
	req := client.Get(srv.URL).WithHeader("X-Custom", "preexisting")

	cfg := relay.SSEClientConfig{
		MaxReconnects:  1,
		ReconnectDelay: 1 * time.Millisecond,
	}
	err := client.ExecuteSSEWithReconnect(req, cfg, func(relay.SSEEvent) bool {
		return true // keep going until MaxReconnects stops the loop
	})
	if err != nil {
		t.Fatalf("ExecuteSSEWithReconnect error: %v", err)
	}

	// Reuse the original req for a plain request after the reconnect loop
	// finished - this is the exact scenario the bug corrupts.
	if _, execErr := client.Execute(req); execErr != nil {
		t.Fatalf("reusing req after ExecuteSSEWithReconnect: %v", execErr)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seenHeaders) != 3 {
		t.Fatalf("expected 3 connections (initial SSE, 1 reconnect, 1 reused plain request), got %d", len(seenHeaders))
	}
	if got := seenHeaders[0].Get("Last-Event-Id"); got != "" {
		t.Errorf("first connection should not carry Last-Event-Id yet, got %q", got)
	}
	if got := seenHeaders[1].Get("Last-Event-Id"); got != "1" {
		t.Errorf("reconnect should carry Last-Event-Id=1, got %q", got)
	}
	if got := seenHeaders[2].Get("Last-Event-Id"); got != "" {
		t.Errorf("reusing the original req after ExecuteSSEWithReconnect leaked Last-Event-Id = %q onto it", got)
	}
	if got := seenHeaders[2].Get("X-Custom"); got != "preexisting" {
		t.Errorf("reused req lost its own pre-existing header: X-Custom = %q, want %q", got, "preexisting")
	}
}

func TestExecuteSSEWithReconnect_EventTypeFiltering(t *testing.T) {
	raw := "event: update\ndata: wanted\n\nevent: ignored\ndata: not-wanted\n\nevent: update\ndata: wanted-too\n\n"
	srv := sseServer(raw)
	defer srv.Close()

	client := relay.New()
	var received []relay.SSEEvent
	cfg := relay.SSEClientConfig{
		EventTypes:     []string{"update"},
		ReconnectDelay: 1 * time.Millisecond,
	}
	err := client.ExecuteSSEWithReconnect(
		client.Get(srv.URL),
		cfg,
		func(ev relay.SSEEvent) bool {
			received = append(received, ev)
			return len(received) < 2
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(received) != 2 {
		t.Fatalf("expected 2 events, got %d", len(received))
	}
	for _, ev := range received {
		if ev.Event != "update" {
			t.Errorf("event type = %q, want %q", ev.Event, "update")
		}
	}
	for _, data := range []string{"wanted", "wanted-too"} {
		found := false
		for _, ev := range received {
			if ev.Data == data {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("data %q not found in received events", data)
		}
	}
}

func TestExecuteSSEStream_BasicUsage(t *testing.T) {
	raw := "data: first\n\ndata: second\n\ndata: third\n\n"
	srv := sseServer(raw)
	defer srv.Close()

	client := relay.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, errs := client.ExecuteSSEStream(ctx, client.Get(srv.URL))

	var received []relay.SSEEvent
loop:
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				break loop
			}
			received = append(received, ev)
		case err, ok := <-errs:
			if !ok {
				break loop
			}
			t.Fatalf("unexpected error: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("timeout waiting for event")
		}
	}

	if len(received) != 3 {
		t.Fatalf("expected 3 events, got %d", len(received))
	}
	wantData := []string{"first", "second", "third"}
	for i, want := range wantData {
		if received[i].Data != want {
			t.Errorf("event[%d].Data = %q, want %q", i, received[i].Data, want)
		}
	}
}

func TestExecuteSSEStream_ContextCancellation(t *testing.T) {
	raw := "data: one\n\n"
	srv := sseServer(raw)
	defer srv.Close()

	client := relay.New()
	ctx, cancel := context.WithCancel(context.Background())

	events, errs := client.ExecuteSSEStream(ctx, client.Get(srv.URL))

	select {
	case ev := <-events:
		if ev.Data != "one" {
			t.Errorf("data = %q, want %q", ev.Data, "one")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for first event")
	}

	cancel()

	select {
	case <-events:
	case <-errs:
	case <-time.After(5 * time.Second):
	}
}

// TestExecuteSSEStream_ContextCancellationUnblocksPendingRead guards
// against ctx never being attached to the underlying request - the doc
// comment says "close ctx to stop the stream," and there's a ctx.Done()
// check between scanner.Scan() calls, but that only fires between reads.
// For a real long-lived SSE connection idle between events (the whole
// point of the API), scanner.Scan() blocks on the socket Read(), governed
// by whatever context the request carries - not ctx, unless ctx is
// actually attached to it. Unlike TestExecuteSSEStream_ContextCancellation
// (whose server returns immediately, reaching body EOF on its own
// regardless of cancellation), this server blocks after the first event
// without ever closing the connection on its own, so only a correctly
// wired cancellation can unblock the client.
func TestExecuteSSEStream_ContextCancellationUnblocksPendingRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: one\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Block until the client disconnects - simulates a long-lived idle
		// SSE connection with no more data, never closing on its own.
		<-r.Context().Done()
	}))
	defer srv.Close()

	client := relay.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, errs := client.ExecuteSSEStream(ctx, client.Get(srv.URL))

	select {
	case ev := <-events:
		if ev.Data != "one" {
			t.Fatalf("data = %q, want %q", ev.Data, "one")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for first event")
	}

	// The scanner is now blocked on a socket Read() waiting for more data
	// that will never come - exactly the scenario the bug affected.
	cancel()

	done := make(chan struct{})
	go func() {
		for range events { //nolint:revive
		}
		for range errs { //nolint:revive
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("channels did not close within 5s after ctx cancellation - the pending read was not interrupted")
	}
}

func TestExecuteSSEStream_AllFieldsPreserved(t *testing.T) {
	raw := "id: 123\nevent: custom\ndata: payload\nretry: 5000\n\n"
	srv := sseServer(raw)
	defer srv.Close()

	client := relay.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, errs := client.ExecuteSSEStream(ctx, client.Get(srv.URL))

	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("events channel closed prematurely")
		}
		if ev.ID != "123" {
			t.Errorf("ID = %q, want %q", ev.ID, "123")
		}
		if ev.Event != "custom" {
			t.Errorf("Event = %q, want %q", ev.Event, "custom")
		}
		if ev.Data != "payload" {
			t.Errorf("Data = %q, want %q", ev.Data, "payload")
		}
		if ev.Retry != 5000 {
			t.Errorf("Retry = %d, want 5000", ev.Retry)
		}
	case err, ok := <-errs:
		if ok {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}
