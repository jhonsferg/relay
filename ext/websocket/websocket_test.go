package websocket_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/jhonsferg/relay"
	ws "github.com/jhonsferg/relay/ext/websocket"
)

// upgrader is a shared gorilla upgrader for test servers.
var upgrader = gorilla.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// echoServer returns a test WebSocket server that echoes each received message.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if writeErr := conn.WriteMessage(mt, msg); writeErr != nil {
				return
			}
		}
	}))
	return srv
}

// headerEchoServer returns a test WebSocket server that sends back the value
// of the X-Auth header as the first text message.
func headerEchoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.WriteMessage(gorilla.TextMessage, []byte(r.Header.Get("X-Auth")))
	}))
	return srv
}

// wsURL converts an http:// test server URL to ws://.
func wsURL(httpURL string) string {
	return strings.Replace(httpURL, "http://", "ws://", 1)
}

// TestDial_Echo verifies a basic connect, write, read cycle.
func TestDial_Echo(t *testing.T) {
	t.Parallel()
	srv := echoServer(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := ws.Dial(ctx, wsURL(srv.URL))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	const want = "hello relay"
	if err := conn.WriteText(want); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if string(msg.Data) != want {
		t.Errorf("got %q, want %q", msg.Data, want)
	}
	if msg.Type != ws.TextMessage {
		t.Errorf("type = %d, want TextMessage (%d)", msg.Type, ws.TextMessage)
	}
}

// TestDial_WriteBytes verifies binary frame sending.
func TestDial_WriteBytes(t *testing.T) {
	t.Parallel()
	srv := echoServer(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := ws.Dial(ctx, wsURL(srv.URL))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	payload := []byte{0x01, 0x02, 0x03}
	if err := conn.WriteBytes(payload); err != nil {
		t.Fatalf("WriteBytes: %v", err)
	}
	msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if string(msg.Data) != string(payload) {
		t.Errorf("got %v, want %v", msg.Data, payload)
	}
	if msg.Type != ws.BinaryMessage {
		t.Errorf("type = %d, want BinaryMessage (%d)", msg.Type, ws.BinaryMessage)
	}
}

// TestDial_WithHeaders verifies that static headers are sent during the upgrade.
func TestDial_WithHeaders(t *testing.T) {
	t.Parallel()
	srv := headerEchoServer(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	headers := http.Header{"X-Auth": []string{"test-token"}}
	conn, err := ws.Dial(ctx, wsURL(srv.URL), ws.WithHeaders(headers))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if string(msg.Data) != "test-token" {
		t.Errorf("X-Auth echoed as %q, want \"test-token\"", msg.Data)
	}
}

// TestDial_WithSigner verifies that the relay.RequestSigner is applied to the
// upgrade handshake, injecting headers before the connection is established.
func TestDial_WithSigner(t *testing.T) {
	t.Parallel()
	srv := headerEchoServer(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	signer := relay.RequestSignerFunc(func(r *http.Request) error {
		r.Header.Set("X-Auth", "signed-bearer")
		return nil
	})

	conn, err := ws.Dial(ctx, wsURL(srv.URL), ws.WithSigner(signer))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if string(msg.Data) != "signed-bearer" {
		t.Errorf("X-Auth echoed as %q, want \"signed-bearer\"", msg.Data)
	}
}

// TestDial_InvalidURL verifies that an invalid URL returns an error.
func TestDial_InvalidURL(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := ws.Dial(ctx, "ws://127.0.0.1:0/nonexistent")
	if err == nil {
		t.Fatal("expected error for unreachable server, got nil")
	}
}

// TestConn_Underlying verifies that the underlying gorilla connection is exposed.
func TestConn_Underlying(t *testing.T) {
	t.Parallel()
	srv := echoServer(t)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := ws.Dial(ctx, wsURL(srv.URL))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if conn.Underlying() == nil {
		t.Error("Underlying() returned nil")
	}
}
