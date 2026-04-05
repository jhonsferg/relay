package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	xwebsocket "golang.org/x/net/websocket"
)

// echoWSHandler echoes binary messages back to the sender.
var echoWSHandler = xwebsocket.Handler(func(ws *xwebsocket.Conn) {
	var msg []byte
	for {
		if err := xwebsocket.Message.Receive(ws, &msg); err != nil {
			return
		}
		_ = xwebsocket.Message.Send(ws, msg)
	}
})

// newWSTestServer starts an httptest.Server with a WebSocket echo endpoint at
// "/" and returns it. The URL is the base HTTP URL; callers rewrite to ws://.
func newWSTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/ws", echoWSHandler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// wsURL converts an http:// test server URL to a ws:// WebSocket URL.
func wsURL(base, path string) string {
	return "ws" + strings.TrimPrefix(base, "http") + path
}

func TestExecuteWebSocket_Upgrade(t *testing.T) {
	srv := newWSTestServer(t)

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	conn, err := c.ExecuteWebSocket(context.Background(), c.Get(wsURL(srv.URL, "/ws")))
	if err != nil {
		t.Fatalf("ExecuteWebSocket: %v", err)
	}
	defer conn.Close() //nolint:errcheck
}

func TestExecuteWebSocket_ReadWrite(t *testing.T) {
	srv := newWSTestServer(t)

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	conn, err := c.ExecuteWebSocket(context.Background(), c.Get(wsURL(srv.URL, "/ws")))
	if err != nil {
		t.Fatalf("ExecuteWebSocket: %v", err)
	}
	defer conn.Close() //nolint:errcheck

	ctx := context.Background()
	want := []byte("hello websocket")
	if writeErr := conn.Write(ctx, want); writeErr != nil {
		t.Fatalf("Write: %v", writeErr)
	}

	got, readErr := conn.Read(ctx)
	if readErr != nil {
		t.Fatalf("Read: %v", readErr)
	}
	if string(got) != string(want) {
		t.Errorf("echo mismatch: got %q, want %q", got, want)
	}
}

func TestExecuteWebSocket_Close(t *testing.T) {
	srv := newWSTestServer(t)

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	conn, err := c.ExecuteWebSocket(context.Background(), c.Get(wsURL(srv.URL, "/ws")))
	if err != nil {
		t.Fatalf("ExecuteWebSocket: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestExecuteWebSocket_NilRequest(t *testing.T) {
	c := New()
	_, err := c.ExecuteWebSocket(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil request")
	}
}

func TestExecuteWebSocket_ClosedClient(t *testing.T) {
	c := New()
	_ = c.Shutdown(context.Background())
	_, err := c.ExecuteWebSocket(context.Background(), c.Get("ws://localhost/ws"))
	if err == nil {
		t.Fatal("expected error from closed client")
	}
}

func TestExecuteWebSocket_Timeout(t *testing.T) {
	// Use a server that never responds to trigger the dial timeout.
	c := New(
		WithWebSocketDialTimeout(50*time.Millisecond),
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
	)
	// Dial an address that is not listening.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := c.ExecuteWebSocket(ctx, c.Get("ws://127.0.0.1:19999/ws"))
	if err == nil {
		t.Fatal("expected error dialling non-listening port")
	}
}

func TestExecuteWebSocket_DefaultHeaders(t *testing.T) {
	captured := make(chan http.Header, 1)
	mux := http.NewServeMux()
	mux.Handle("/ws", xwebsocket.Handler(func(ws *xwebsocket.Conn) {
		captured <- ws.Request().Header.Clone()
	}))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(
		WithDefaultHeaders(map[string]string{"X-Relay-Test": "value123"}),
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
	)
	conn, err := c.ExecuteWebSocket(context.Background(), c.Get(wsURL(srv.URL, "/ws")))
	if err != nil {
		t.Fatalf("ExecuteWebSocket: %v", err)
	}
	defer conn.Close() //nolint:errcheck

	select {
	case h := <-captured:
		if got := h.Get("X-Relay-Test"); got != "value123" {
			t.Errorf("expected X-Relay-Test=value123, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for WebSocket handler to receive headers")
	}
}

func TestWSDialTimeout_FallsBackToClientTimeout(t *testing.T) {
	cfg := &Config{Timeout: 5 * time.Second}
	if got := wsDialTimeout(cfg); got != 5*time.Second {
		t.Errorf("expected client timeout fallback, got %v", got)
	}

	cfg2 := &Config{Timeout: 5 * time.Second, WebSocketDialTimeout: 2 * time.Second}
	if got := wsDialTimeout(cfg2); got != 2*time.Second {
		t.Errorf("expected explicit ws timeout, got %v", got)
	}
}

func TestWithWebSocketDialTimeout(t *testing.T) {
	c := New(WithWebSocketDialTimeout(3 * time.Second))
	if c.config.WebSocketDialTimeout != 3*time.Second {
		t.Errorf("expected 3s, got %v", c.config.WebSocketDialTimeout)
	}
}

func TestExecuteWebSocket_HTTPURLRewrite(t *testing.T) {
	// http:// URLs should be transparently rewritten to ws://.
	srv := newWSTestServer(t)

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	// Pass the http:// URL directly - client should rewrite to ws://.
	conn, err := c.ExecuteWebSocket(context.Background(), c.Get(srv.URL+"/ws"))
	if err != nil {
		t.Fatalf("ExecuteWebSocket with http URL: %v", err)
	}
	defer conn.Close() //nolint:errcheck
}
