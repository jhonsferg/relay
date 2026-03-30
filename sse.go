package relay

import (
	"bufio"
	"strconv"
	"strings"
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
