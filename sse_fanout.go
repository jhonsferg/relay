package relay

import (
	"context"
	"sync"
	"time"
)

const defaultFanOutBufferSize = 64

// SSEFanOut connects to a single SSE stream and multiplexes events to
// multiple concurrent subscribers. Only one upstream HTTP connection is
// maintained regardless of how many subscribers are active.
//
// Slow subscribers whose channel buffer becomes full are automatically
// removed and their channels closed. All other subscribers continue
// receiving events unaffected.
//
// Typical usage:
//
//	fo := relay.NewSSEFanOut(client, client.Get("/events"), 64)
//
//	ch1 := fo.Subscribe()
//	ch2 := fo.Subscribe()
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	go func() { _ = fo.Start(ctx) }()
//
//	for ev := range ch1 {
//	    fmt.Println(ev.Data)
//	}
type SSEFanOut struct {
	client     *Client
	req        *Request
	bufferSize int

	mu          sync.RWMutex
	subscribers map[<-chan SSEEvent]chan SSEEvent

	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewSSEFanOut creates a new fan-out multiplexer. The client and req define
// the upstream SSE source. bufferSize is the per-subscriber channel buffer;
// it defaults to 64 when 0.
func NewSSEFanOut(client *Client, req *Request, bufferSize int) *SSEFanOut {
	if bufferSize == 0 {
		bufferSize = defaultFanOutBufferSize
	}
	return &SSEFanOut{
		client:      client,
		req:         req,
		bufferSize:  bufferSize,
		subscribers: make(map[<-chan SSEEvent]chan SSEEvent),
		stopCh:      make(chan struct{}),
	}
}

// Subscribe registers a new subscriber and returns a read-only channel that
// receives SSE events. The channel is closed when the fan-out stops or the
// subscriber is removed due to being slow.
func (f *SSEFanOut) Subscribe() <-chan SSEEvent {
	ch := make(chan SSEEvent, f.bufferSize)
	f.mu.Lock()
	f.subscribers[ch] = ch
	f.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber. The channel returned by Subscribe is
// closed immediately after removal.
func (f *SSEFanOut) Unsubscribe(ch <-chan SSEEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if bidi, ok := f.subscribers[ch]; ok {
		delete(f.subscribers, ch)
		close(bidi)
	}
}

// SubscriberCount returns the number of currently active subscribers.
func (f *SSEFanOut) SubscriberCount() int {
	f.mu.RLock()
	n := len(f.subscribers)
	f.mu.RUnlock()
	return n
}

// Start begins consuming the upstream SSE stream and distributing events to
// all active subscribers. It blocks until ctx is cancelled, Stop is called,
// or the upstream returns a non-recoverable error. Temporary disconnections
// trigger automatic reconnects after a short delay.
//
// All subscriber channels are closed when Start returns.
func (f *SSEFanOut) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		f.closeAll()
	}()

	// Propagate Stop() into the derived context.
	go func() {
		select {
		case <-f.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	reconnectDelay := 3 * time.Second

	for {
		// Shallow-copy the request and attach the cancellable context so the
		// underlying HTTP transport tears down the connection on cancellation,
		// allowing the blocking scanner to return rather than waiting forever.
		attempt := *f.req
		attempt = *attempt.WithContext(ctx)
		events, errs := f.client.ExecuteSSEStream(ctx, &attempt)

		var streamErr error
		for events != nil || errs != nil {
			select {
			case <-ctx.Done():
				return nil
			case ev, ok := <-events:
				if !ok {
					events = nil
					continue
				}
				f.dispatch(ev)
			case err, ok := <-errs:
				if !ok {
					errs = nil
					continue
				}
				// Treat context errors as a normal shutdown, not a hard error.
				if ctx.Err() != nil {
					return nil
				}
				streamErr = err
				errs = nil
				events = nil
			}
		}

		if streamErr != nil {
			return streamErr
		}

		// Stream ended cleanly - wait before reconnecting.
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(reconnectDelay):
		}
	}
}

// Stop signals the fan-out to halt and closes all subscriber channels.
// It is safe to call Stop concurrently with Start and from multiple goroutines;
// only the first call has any effect.
func (f *SSEFanOut) Stop() {
	f.stopOnce.Do(func() {
		close(f.stopCh)
	})
}

// dispatch sends ev to every subscriber using a non-blocking send. Subscribers
// whose buffer is full are collected and removed after the broadcast.
func (f *SSEFanOut) dispatch(ev SSEEvent) {
	var toDrop []chan SSEEvent

	f.mu.RLock()
	for _, ch := range f.subscribers {
		select {
		case ch <- ev:
		default:
			toDrop = append(toDrop, ch)
		}
	}
	f.mu.RUnlock()

	if len(toDrop) == 0 {
		return
	}

	f.mu.Lock()
	for _, ch := range toDrop {
		if _, ok := f.subscribers[ch]; ok {
			delete(f.subscribers, ch)
			close(ch)
		}
	}
	f.mu.Unlock()
}

// closeAll closes every subscriber channel and empties the subscriber map.
// It is safe to call multiple times; subsequent calls are no-ops.
func (f *SSEFanOut) closeAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for key, ch := range f.subscribers {
		delete(f.subscribers, key)
		close(ch)
	}
}
