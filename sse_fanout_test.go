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

// streamingSSEServer returns a test server that sends the provided events one
// by one (with an optional inter-event delay) and then closes the connection.
func streamingSSEServer(events []string, delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		for _, ev := range events {
			select {
			case <-r.Context().Done():
				return
			default:
			}
			fmt.Fprintf(w, "data: %s\n\n", ev)
			flusher.Flush()
			if delay > 0 {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(delay):
				}
			}
		}
		// Connection closes when the handler returns.
	}))
}

func TestSSEFanOut_SingleSubscriber(t *testing.T) {
	events := []string{"alpha", "beta", "gamma"}
	srv := streamingSSEServer(events, 0)

	client := relay.New()
	fo := relay.NewSSEFanOut(client, client.Get(srv.URL), 16)

	ch := fo.Subscribe()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	startDone := make(chan struct{})
	go func() {
		defer close(startDone)
		_ = fo.Start(ctx)
	}()

	var received []string
	for _, want := range events {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed before receiving %q", want)
			}
			received = append(received, ev.Data)
		case <-ctx.Done():
			t.Fatal("timeout waiting for event")
		}
	}

	// Stop fan-out before closing server so the SSE connection is released.
	cancel()
	select {
	case <-startDone:
	case <-time.After(3 * time.Second):
		t.Error("fan-out did not stop within timeout")
	}
	srv.Close()

	if len(received) != 3 {
		t.Fatalf("expected 3 events, got %d: %v", len(received), received)
	}
	for i, want := range events {
		if received[i] != want {
			t.Errorf("event[%d] = %q, want %q", i, received[i], want)
		}
	}
}

func TestSSEFanOut_MultipleSubscribers(t *testing.T) {
	events := []string{"one", "two", "three"}
	srv := streamingSSEServer(events, 5*time.Millisecond)

	client := relay.New()
	fo := relay.NewSSEFanOut(client, client.Get(srv.URL), 16)

	const numSubs = 5
	channels := make([]<-chan relay.SSEEvent, numSubs)
	for i := range channels {
		channels[i] = fo.Subscribe()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	startDone := make(chan struct{})
	go func() {
		defer close(startDone)
		_ = fo.Start(ctx)
	}()

	var wg sync.WaitGroup
	for _, ch := range channels {
		ch := ch
		wg.Add(1)
		go func() {
			defer wg.Done()
			var got []string
			for _, want := range events {
				select {
				case ev, ok := <-ch:
					if !ok {
						t.Errorf("channel closed before receiving %q", want)
						return
					}
					got = append(got, ev.Data)
				case <-ctx.Done():
					t.Error("timeout waiting for event")
					return
				}
			}
			for i, want := range events {
				if got[i] != want {
					t.Errorf("event[%d] = %q, want %q", i, got[i], want)
				}
			}
		}()
	}

	wg.Wait()

	// Stop fan-out before closing server so the SSE connection is released.
	cancel()
	select {
	case <-startDone:
	case <-time.After(3 * time.Second):
		t.Error("fan-out did not stop within timeout")
	}
	srv.Close()
}

func TestSSEFanOut_SlowSubscriberDropped(t *testing.T) {
	// bufferSize=2: slow sub can hold 2 events then is dropped on the 3rd.
	const numEvents = 10
	events := make([]string, numEvents)
	for i := range events {
		events[i] = fmt.Sprintf("ev%d", i)
	}

	srv := streamingSSEServer(events, 0)
	defer srv.Close()

	client := relay.New()
	fo := relay.NewSSEFanOut(client, client.Get(srv.URL), 2)

	slow := fo.Subscribe()
	fast := fo.Subscribe()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() { _ = fo.Start(ctx) }()

	// Fast subscriber drains all available events, verifying it is not
	// interrupted by the slow subscriber being dropped.
	var fastReceived []string
	deadline := time.After(3 * time.Second)
fastDrain:
	for {
		select {
		case ev, ok := <-fast:
			if !ok {
				break fastDrain
			}
			fastReceived = append(fastReceived, ev.Data)
			if len(fastReceived) >= numEvents {
				break fastDrain
			}
		case <-deadline:
			break fastDrain
		}
	}

	if len(fastReceived) == 0 {
		t.Fatal("fast subscriber received no events")
	}

	// Slow subscriber channel must have been closed by the fan-out.
	// Drain whatever was buffered and wait for close.
	slowClosed := false
	drainDeadline := time.After(2 * time.Second)
drainSlow:
	for {
		select {
		case _, ok := <-slow:
			if !ok {
				slowClosed = true
				break drainSlow
			}
		case <-drainDeadline:
			break drainSlow
		}
	}

	if !slowClosed {
		t.Error("slow subscriber channel was not closed after falling behind")
	}
}

func TestSSEFanOut_Unsubscribe(t *testing.T) {
	srv := streamingSSEServer([]string{"ping"}, 0)
	defer srv.Close()

	client := relay.New()
	fo := relay.NewSSEFanOut(client, client.Get(srv.URL), 8)

	ch := fo.Subscribe()

	if fo.SubscriberCount() != 1 {
		t.Fatalf("expected 1 subscriber before unsubscribe, got %d", fo.SubscriberCount())
	}

	fo.Unsubscribe(ch)

	if fo.SubscriberCount() != 0 {
		t.Fatalf("expected 0 subscribers after unsubscribe, got %d", fo.SubscriberCount())
	}

	// Channel must be closed after unsubscribe.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed, but received a value")
		}
	case <-time.After(time.Second):
		t.Error("channel was not closed after Unsubscribe")
	}
}

func TestSSEFanOut_StopClosesAllChannels(t *testing.T) {
	srv := streamingSSEServer(nil, 0)
	defer srv.Close()

	client := relay.New()
	fo := relay.NewSSEFanOut(client, client.Get(srv.URL), 8)

	const numSubs = 4
	channels := make([]<-chan relay.SSEEvent, numSubs)
	for i := range channels {
		channels[i] = fo.Subscribe()
	}

	ctx := context.Background()
	started := make(chan struct{})
	go func() {
		close(started)
		_ = fo.Start(ctx)
	}()

	<-started
	time.Sleep(50 * time.Millisecond) // let Start connect

	fo.Stop()

	// All subscriber channels must close within a reasonable timeout.
	for i, ch := range channels {
		select {
		case _, ok := <-ch:
			if ok {
				t.Errorf("channel[%d]: expected closed, got value", i)
			}
		case <-time.After(3 * time.Second):
			t.Errorf("channel[%d] was not closed after Stop()", i)
		}
	}
}

func TestSSEFanOut_SubscriberCount(t *testing.T) {
	client := relay.New()
	fo := relay.NewSSEFanOut(client, client.Get("http://localhost"), 8)

	if fo.SubscriberCount() != 0 {
		t.Fatalf("initial count = %d, want 0", fo.SubscriberCount())
	}

	ch1 := fo.Subscribe()
	if fo.SubscriberCount() != 1 {
		t.Fatalf("count after 1 subscribe = %d, want 1", fo.SubscriberCount())
	}

	ch2 := fo.Subscribe()
	ch3 := fo.Subscribe()
	if fo.SubscriberCount() != 3 {
		t.Fatalf("count after 3 subscribes = %d, want 3", fo.SubscriberCount())
	}

	fo.Unsubscribe(ch2)
	if fo.SubscriberCount() != 2 {
		t.Fatalf("count after 1 unsubscribe = %d, want 2", fo.SubscriberCount())
	}

	fo.Unsubscribe(ch1)
	fo.Unsubscribe(ch3)
	if fo.SubscriberCount() != 0 {
		t.Fatalf("count after all unsubscribes = %d, want 0", fo.SubscriberCount())
	}
}

func TestSSEFanOut_StartCancelledContext(t *testing.T) {
	srv := streamingSSEServer(nil, 0)
	defer srv.Close()

	client := relay.New()
	fo := relay.NewSSEFanOut(client, client.Get(srv.URL), 8)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- fo.Start(ctx)
	}()

	// Give Start a moment to connect, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		// nil or context.Canceled are both acceptable.
		_ = err
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return after context cancellation")
	}
}

func TestSSEFanOut_DefaultBufferSize(t *testing.T) {
	// bufferSize=0 must default to 64.
	client := relay.New()
	fo := relay.NewSSEFanOut(client, client.Get("http://localhost"), 0)

	ch := fo.Subscribe()

	if cap(ch) != 64 {
		t.Fatalf("expected default buffer size 64, got %d", cap(ch))
	}
}
