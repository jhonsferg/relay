package relay

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	xwebsocket "golang.org/x/net/websocket"
)

// WSConn is an active WebSocket connection created by [Client.ExecuteWebSocket].
// It reuses the relay client's TLS configuration, default headers, and
// request signer so auth is consistent across HTTP and WebSocket calls.
//
// WSConn is not safe for concurrent reads or concurrent writes, but a read
// and a write may proceed simultaneously from different goroutines.
type WSConn struct {
	ws *xwebsocket.Conn
}

// Read reads the next binary or text message from the peer. It blocks until
// a message arrives, the connection is closed, or ctx is cancelled.
func (c *WSConn) Read(ctx context.Context) ([]byte, error) {
	type result struct {
		data []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		var msg []byte
		if err := xwebsocket.Message.Receive(c.ws, &msg); err != nil {
			ch <- result{nil, err}
			return
		}
		ch <- result{msg, nil}
	}()
	select {
	case r := <-ch:
		return r.data, r.err
	case <-ctx.Done():
		_ = c.ws.Close()
		return nil, ctx.Err()
	}
}

// Write sends data as a binary message to the peer.
func (c *WSConn) Write(ctx context.Context, data []byte) error {
	type result struct{ err error }
	ch := make(chan result, 1)
	go func() {
		ch <- result{xwebsocket.Message.Send(c.ws, data)}
	}()
	select {
	case r := <-ch:
		return r.err
	case <-ctx.Done():
		_ = c.ws.Close()
		return ctx.Err()
	}
}

// Close closes the underlying WebSocket connection.
func (c *WSConn) Close() error { return c.ws.Close() }

// ExecuteWebSocket upgrades a request to a WebSocket connection. req must
// target a "ws://" or "wss://" URL (or an "http://"/"https://" URL which is
// transparently rewritten). The client's TLS config, default headers, and
// request signer are applied to the upgrade handshake.
//
// The caller is responsible for calling [WSConn.Close] when done.
func (c *Client) ExecuteWebSocket(ctx context.Context, req *Request) (*WSConn, error) {
	if req == nil {
		return nil, ErrNilRequest
	}
	if c.closed.Load() {
		return nil, ErrClientClosed
	}

	rawURL := req.rawURL
	if c.config.BaseURL != "" && !strings.HasPrefix(rawURL, "http://") &&
		!strings.HasPrefix(rawURL, "https://") &&
		!strings.HasPrefix(rawURL, "ws://") &&
		!strings.HasPrefix(rawURL, "wss://") {
		rawURL = strings.TrimSuffix(c.config.BaseURL, "/") + "/" + strings.TrimPrefix(rawURL, "/")
	}

	// Rewrite http(s) to ws(s) if needed.
	switch {
	case strings.HasPrefix(rawURL, "http://"):
		rawURL = "ws" + rawURL[4:]
	case strings.HasPrefix(rawURL, "https://"):
		rawURL = "wss" + rawURL[5:]
	}

	wsURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("websocket: invalid URL %q: %w", rawURL, err)
	}

	// Build a synthetic origin from the target URL (required by x/net/websocket).
	origin := &url.URL{Scheme: "http", Host: wsURL.Host}
	if wsURL.Scheme == "wss" {
		origin.Scheme = "https"
	}

	cfg, cfgErr := xwebsocket.NewConfig(rawURL, origin.String())
	if cfgErr != nil {
		return nil, fmt.Errorf("websocket: build config: %w", cfgErr)
	}

	// Copy TLS config from the relay client.
	if c.config.TLSConfig != nil {
		cfg.TlsConfig = c.config.TLSConfig.Clone()
	}

	// Apply dial timeout.
	dialTimeout := c.config.WebSocketDialTimeout
	if dialTimeout == 0 {
		dialTimeout = c.config.Timeout
	}
	if dialTimeout > 0 {
		cfg.Dialer = &net.Dialer{Timeout: dialTimeout}
	}

	// Merge default headers from the client config.
	for k, v := range c.config.DefaultHeaders {
		cfg.Header.Set(k, v)
	}

	// Merge per-request headers (override defaults).
	for k, v := range req.headers {
		cfg.Header.Set(k, v)
	}

	// Apply signer to a synthetic *http.Request so the same auth path is used.
	if c.config.Signer != nil {
		synthReq, synthErr := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if synthErr != nil {
			return nil, fmt.Errorf("websocket: build synthetic request: %w", synthErr)
		}
		synthReq.Header = cfg.Header
		if signErr := c.config.Signer.Sign(synthReq); signErr != nil {
			return nil, fmt.Errorf("websocket: signer: %w", signErr)
		}
		cfg.Header = synthReq.Header
	}

	ws, dialErr := cfg.DialContext(ctx)
	if dialErr != nil {
		return nil, fmt.Errorf("websocket: dial %s: %w", rawURL, dialErr)
	}
	return &WSConn{ws: ws}, nil
}

// wsDialTimeout returns the effective WebSocket dial timeout for the given
// config. Exported for test access.
func wsDialTimeout(cfg *Config) time.Duration {
	if cfg.WebSocketDialTimeout > 0 {
		return cfg.WebSocketDialTimeout
	}
	return cfg.Timeout
}
