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

// benchUpgrader is a shared gorilla upgrader for benchmark servers.
var benchUpgrader = gorilla.Upgrader{
	CheckOrigin: func(_ *http.Request) bool { return true },
}

// benchEchoServer returns a WebSocket server that echoes each received
// message, for use by the benchmarks below.
func benchEchoServer(b *testing.B) *httptest.Server {
	b.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := benchUpgrader.Upgrade(w, r, nil)
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

func benchWSURL(httpURL string) string {
	return strings.Replace(httpURL, "http://", "ws://", 1)
}

// BenchmarkDial measures the per-connection cost of Dial: building the
// synthetic signing request (when a signer is configured), performing the
// HTTP upgrade handshake, and constructing the Conn wrapper.
func BenchmarkDial(b *testing.B) {
	srv := benchEchoServer(b)
	defer srv.Close()
	url := benchWSURL(srv.URL)

	signer := relay.RequestSignerFunc(func(r *http.Request) error {
		r.Header.Set("Authorization", "Bearer bench-token")
		return nil
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		conn, err := ws.Dial(ctx, url, ws.WithSigner(signer))
		if err != nil {
			cancel()
			b.Fatalf("Dial: %v", err)
		}
		_ = conn.Close()
		cancel()
	}
}

// BenchmarkMessageRoundTrip measures the cost of a single write+read
// exchange over an already-established connection - the steady-state
// per-message hot path once a WebSocket session is up.
func BenchmarkMessageRoundTrip(b *testing.B) {
	srv := benchEchoServer(b)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := ws.Dial(ctx, benchWSURL(srv.URL))
	if err != nil {
		b.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	const payload = "the quick brown fox jumps over the lazy dog"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := conn.WriteText(payload); err != nil {
			b.Fatalf("WriteText: %v", err)
		}
		msg, err := conn.ReadMessage()
		if err != nil {
			b.Fatalf("ReadMessage: %v", err)
		}
		if string(msg.Data) != payload {
			b.Fatalf("got %q, want %q", msg.Data, payload)
		}
	}
}
