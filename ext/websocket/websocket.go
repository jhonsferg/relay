// Package websocket provides WebSocket connection support for services that
// use relay for HTTP communication. It applies the same authentication and
// signing primitives (relay.RequestSigner, custom headers) to the WebSocket
// upgrade handshake, ensuring a consistent authentication story across HTTP
// and WebSocket connections.
//
// # Basic usage
//
//	conn, err := websocket.Dial(ctx, "wss://api.example.com/ws",
//	    websocket.WithSigner(relay.RequestSignerFunc(func(r *http.Request) error {
//	        r.Header.Set("Authorization", "Bearer "+token)
//	        return nil
//	    })),
//	)
//	if err != nil { ... }
//	defer conn.Close()
//
//	// Send a message
//	if err := conn.WriteText("hello"); err != nil { ... }
//
//	// Receive a message
//	msg, err := conn.ReadMessage()
//
// # TLS customisation
//
//	conn, err := websocket.Dial(ctx, "wss://...",
//	    websocket.WithTLSConfig(&tls.Config{
//	        InsecureSkipVerify: false,
//	        RootCAs:            certPool,
//	    }),
//	)
package websocket

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	gorilla "github.com/gorilla/websocket"
	"github.com/jhonsferg/relay"
)

// MessageType mirrors gorilla/websocket message types for callers that need
// to distinguish text from binary frames.
const (
	// TextMessage denotes a text data message (UTF-8 encoded).
	TextMessage = gorilla.TextMessage
	// BinaryMessage denotes a binary data message.
	BinaryMessage = gorilla.BinaryMessage
	// CloseMessage denotes a close control message.
	CloseMessage = gorilla.CloseMessage
	// PingMessage denotes a ping control message.
	PingMessage = gorilla.PingMessage
	// PongMessage denotes a pong control message.
	PongMessage = gorilla.PongMessage
)

// Message is a WebSocket message received from the peer.
type Message struct {
	// Type is the message type (TextMessage or BinaryMessage).
	Type int
	// Data is the raw message payload.
	Data []byte
}

// Conn is an active WebSocket connection. Call Close or CloseGracefully
// when done.
type Conn struct {
	ws *gorilla.Conn
}

// option holds the configuration for Dial.
type option struct {
	headers   http.Header
	signer    relay.RequestSigner
	tlsConfig *tls.Config
}

// Option configures a WebSocket [Dial] call.
type Option func(*option)

// WithHeaders adds static headers to the WebSocket upgrade handshake.
// Calling WithHeaders multiple times merges all provided headers.
func WithHeaders(h http.Header) Option {
	return func(o *option) {
		if o.headers == nil {
			o.headers = h.Clone()
			return
		}
		for k, vals := range h {
			for _, v := range vals {
				o.headers.Add(k, v)
			}
		}
	}
}

// WithSigner sets a [relay.RequestSigner] that signs the upgrade HTTP request
// before the handshake is sent. This mirrors [relay.WithSigner] so the same
// signer implementation can be reused for both HTTP and WebSocket connections.
func WithSigner(s relay.RequestSigner) Option {
	return func(o *option) { o.signer = s }
}

// WithTLSConfig sets a custom TLS configuration for the upgrade connection.
func WithTLSConfig(cfg *tls.Config) Option {
	return func(o *option) { o.tlsConfig = cfg }
}

// Dial establishes a WebSocket connection to url. It performs the HTTP upgrade
// handshake, applying any configured headers and signer before sending.
//
// url must use the "ws://" or "wss://" scheme.
//
// Dial blocks until the handshake completes or ctx is cancelled.
func Dial(ctx context.Context, url string, opts ...Option) (*Conn, error) {
	cfg := &option{}
	for _, o := range opts {
		o(cfg)
	}

	headers := cfg.headers.Clone()
	if headers == nil {
		headers = make(http.Header)
	}

	// If a signer is configured, create a synthetic *http.Request so the
	// signer can inject headers using the same interface as relay.Client.
	if cfg.signer != nil {
		synth, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("websocket: build synthetic request: %w", err)
		}
		synth.Header = headers
		if signErr := cfg.signer.Sign(synth); signErr != nil {
			return nil, fmt.Errorf("websocket: signer: %w", signErr)
		}
		headers = synth.Header
	}

	dialer := gorilla.Dialer{
		TLSClientConfig:  cfg.tlsConfig,
		HandshakeTimeout: 0, // governed by ctx deadline
	}

	ws, _, err := dialer.DialContext(ctx, url, headers)
	if err != nil {
		return nil, fmt.Errorf("websocket: dial %s: %w", url, err)
	}
	return &Conn{ws: ws}, nil
}

// WriteText sends a UTF-8 text frame to the peer.
func (c *Conn) WriteText(text string) error {
	if err := c.ws.WriteMessage(gorilla.TextMessage, []byte(text)); err != nil {
		return fmt.Errorf("websocket: write text: %w", err)
	}
	return nil
}

// WriteBytes sends a binary frame to the peer.
func (c *Conn) WriteBytes(data []byte) error {
	if err := c.ws.WriteMessage(gorilla.BinaryMessage, data); err != nil {
		return fmt.Errorf("websocket: write bytes: %w", err)
	}
	return nil
}

// ReadMessage reads the next message from the peer. It blocks until a message
// arrives, the connection is closed, or an error occurs.
func (c *Conn) ReadMessage() (Message, error) {
	t, data, err := c.ws.ReadMessage()
	if err != nil {
		return Message{}, fmt.Errorf("websocket: read: %w", err)
	}
	return Message{Type: t, Data: data}, nil
}

// Close sends a close frame and then closes the underlying connection
// immediately. For a graceful shutdown, use [CloseGracefully].
func (c *Conn) Close() error {
	return c.ws.Close()
}

// CloseGracefully sends a close control message with the given code and
// reason, then waits up to 5 seconds for the peer to echo a close frame
// before closing the underlying connection (RFC 6455 §7.1.2).
//
// Use gorilla/websocket close codes (e.g. 1000 for normal closure,
// 1001 for going away).
func (c *Conn) CloseGracefully(code int, reason string) error {
	const closeWait = 5 * time.Second

	msg := gorilla.FormatCloseMessage(code, reason)
	if err := c.ws.WriteMessage(gorilla.CloseMessage, msg); err != nil {
		_ = c.ws.Close()
		return fmt.Errorf("websocket: write close: %w", err)
	}

	// Drain messages until the peer echoes the close frame or the deadline
	// expires. Per RFC 6455 §7.1.2 the closing handshake is complete only
	// after both sides have sent and received a close frame.
	_ = c.ws.SetReadDeadline(time.Now().Add(closeWait))
	for {
		if _, _, err := c.ws.NextReader(); err != nil {
			break
		}
	}

	return c.ws.Close()
}

// Underlying returns the underlying gorilla/websocket connection for advanced
// use cases (setting read/write deadlines, configuring ping handlers, etc.).
func (c *Conn) Underlying() *gorilla.Conn { return c.ws }
