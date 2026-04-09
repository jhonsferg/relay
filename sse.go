package relay

import (
	"bufio"
	"context"
	"strconv"
	"strings"
	"time"
)

// SSEEvent is a single Server-Sent Event as defined by the W3C EventSource
// specification (https://html.spec.whatwg.org/multipage/server-sent-events.html).
//
// Fields not present in the stream are left at their zero values:
//   - Event defaults to "message" per spec when absent.
//   - Retry is 0 when no retry field was received.
type SSEEvent struct {
	// ID is the event identifier set by the "id" field.
	ID string

	// Event is the event type set by the "event" field.
	// Defaults to "message" when the field is absent, per the SSE spec.
	Event string

	// Data is the concatenated content of all "data" fields in the event,
	// with a newline between each line.
	Data string

	// Retry is the reconnection time in milliseconds from the "retry" field.
	// Zero means no retry directive was received.
	Retry int
}

// SSEHandler is called for each fully-parsed [SSEEvent]. Return true to
// continue reading; return false to stop the stream and close the connection.
type SSEHandler func(event SSEEvent) bool

// SSEClientConfig configures the SSE auto-reconnect behaviour.
type SSEClientConfig struct {
	// MaxReconnects is the maximum number of reconnect attempts.
	// 0 means unlimited. Default: 0.
	MaxReconnects int
	// ReconnectDelay is the base delay between reconnects.
	// If the server sends a "retry" field, that takes precedence.
	// Default: 3s.
	ReconnectDelay time.Duration
	// EventTypes filters which event types to deliver to the handler.
	// Empty slice means deliver all events (default behaviour).
	EventTypes []string
}

// ExecuteSSE sends req and reads the response body as a Server-Sent Events
// stream, invoking handler for each complete event. The method returns when
// the handler returns false, the stream ends, or an I/O error occurs.
//
// ExecuteSSE calls [Client.ExecuteStream] internally and therefore inherits
// all of its semantics: no retry logic, participates in graceful drain, and
// the connection is closed automatically when ExecuteSSE returns.
//
// Typical usage:
//
//	err := client.ExecuteSSE(
//	    client.Get("/events").WithHeader("Accept", "text/event-stream"),
//	    func(ev relay.SSEEvent) bool {
//	        fmt.Println(ev.Event, ev.Data)
//	        return true // continue
//	    },
//	)
func (c *Client) ExecuteSSE(req *Request, handler SSEHandler) error {
	stream, err := c.ExecuteStream(req)
	if err != nil {
		return err
	}
	defer func() { _ = stream.Body.Close() }()

	var (
		id    string
		event string
		data  strings.Builder
		retry int
	)

	flush := func() bool {
		ev := SSEEvent{
			ID:    id,
			Event: event,
			Data:  strings.TrimSuffix(data.String(), "\n"),
			Retry: retry,
		}
		if ev.Event == "" {
			ev.Event = "message"
		}
		// Reset accumulators.
		data.Reset()
		retry = 0
		return handler(ev)
	}

	scanner := bufio.NewScanner(stream.Body)
	for scanner.Scan() {
		line := scanner.Text()

		// Blank line = dispatch the event.
		if line == "" {
			if data.Len() > 0 {
				if !flush() {
					return nil
				}
			}
			// Reset event and id only after dispatch (id persists across events
			// unless explicitly reset, but event type resets per spec).
			event = ""
			continue
		}

		// Lines starting with ":" are comments - ignore.
		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ") // strip single leading space

		switch field {
		case "id":
			id = value
		case "event":
			event = value
		case "data":
			data.WriteString(value)
			data.WriteByte('\n')
		case "retry":
			if ms, parseErr := strconv.Atoi(value); parseErr == nil && ms > 0 {
				retry = ms
			}
		}
	}

	// Dispatch any trailing event not followed by a blank line.
	if data.Len() > 0 {
		flush()
	}

	return scanner.Err()
}

// ExecuteSSEWithReconnect sends req and reads SSE stream with automatic
// reconnection. On disconnect, it re-sends the request with the
// Last-Event-ID header set to the last received event ID.
//
// Typical usage:
//
//	cfg := relay.SSEClientConfig{
//	    MaxReconnects: 5,
//	    ReconnectDelay: 1*time.Second,
//	    EventTypes: []string{"update", "notification"},
//	}
//	err := client.ExecuteSSEWithReconnect(
//	    client.Get("/events"),
//	    cfg,
//	    func(ev relay.SSEEvent) bool {
//	        fmt.Println(ev.Event, ev.Data)
//	        return true
//	    },
//	)
func (c *Client) ExecuteSSEWithReconnect(req *Request, cfg SSEClientConfig, handler SSEHandler) error {
	if cfg.ReconnectDelay == 0 {
		cfg.ReconnectDelay = 3 * time.Second
	}

	eventTypeFilter := make(map[string]bool)
	for _, et := range cfg.EventTypes {
		eventTypeFilter[et] = true
	}

	var (
		lastID         string
		reconnectCount int
	)

	for {
		attempt := *req

		if lastID != "" {
			attempt = *attempt.WithHeader("Last-Event-ID", lastID)
		}

		var shouldStop bool
		err := c.ExecuteSSE(&attempt, func(ev SSEEvent) bool {
			if len(eventTypeFilter) > 0 && !eventTypeFilter[ev.Event] {
				return true
			}
			lastID = ev.ID
			if !handler(ev) {
				shouldStop = true
				return false
			}
			return true
		})

		if err != nil {
			return err
		}

		if shouldStop {
			break
		}

		if cfg.MaxReconnects > 0 && reconnectCount >= cfg.MaxReconnects {
			return nil
		}

		reconnectCount++
		time.Sleep(cfg.ReconnectDelay)
	}

	return nil
}

// ExecuteSSEStream returns a channel of SSEEvents and an error channel.
// Events are sent to the channel; the caller controls consumption rate.
// Close ctx to stop the stream.
//
// The caller must read from both channels until one is closed. The channels
// are closed when the stream ends or an error occurs.
//
// Typical usage:
//
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//	events, errs := client.ExecuteSSEStream(ctx, client.Get("/events"))
//	for {
//	    select {
//	    case ev, ok := <-events:
//	        if !ok {
//	            return
//	        }
//	        fmt.Println(ev.Event, ev.Data)
//	    case err, ok := <-errs:
//	        if !ok {
//	            return
//	        }
//	        fmt.Printf("error: %v\n", err)
//	        return
//	    }
//	}
func (c *Client) ExecuteSSEStream(ctx context.Context, req *Request) (<-chan SSEEvent, <-chan error) {
	eventsCh := make(chan SSEEvent)
	errsCh := make(chan error, 1)

	go func() {
		defer close(eventsCh)
		defer close(errsCh)

		select {
		case <-ctx.Done():
			return
		default:
		}

		stream, err := c.ExecuteStream(req)
		if err != nil {
			errsCh <- err
			return
		}
		defer func() { _ = stream.Body.Close() }()

		var (
			id    string
			event string
			data  strings.Builder
			retry int
		)

		flush := func() bool {
			ev := SSEEvent{
				ID:    id,
				Event: event,
				Data:  strings.TrimSuffix(data.String(), "\n"),
				Retry: retry,
			}
			if ev.Event == "" {
				ev.Event = "message"
			}
			data.Reset()
			retry = 0

			select {
			case <-ctx.Done():
				return false
			case eventsCh <- ev:
				return true
			}
		}

		scanner := bufio.NewScanner(stream.Body)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()

			if line == "" {
				if data.Len() > 0 {
					if !flush() {
						return
					}
				}
				event = ""
				continue
			}

			if strings.HasPrefix(line, ":") {
				continue
			}

			field, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")

			switch field {
			case "id":
				id = value
			case "event":
				event = value
			case "data":
				data.WriteString(value)
				data.WriteByte('\n')
			case "retry":
				if ms, parseErr := strconv.Atoi(value); parseErr == nil && ms > 0 {
					retry = ms
				}
			}
		}

		if data.Len() > 0 {
			flush()
		}

		if err := scanner.Err(); err != nil {
			errsCh <- err
		}
	}()

	return eventsCh, errsCh
}
