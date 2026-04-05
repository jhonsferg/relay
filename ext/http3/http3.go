// Package http3 integrates HTTP/3 (QUIC) transport into the relay HTTP client.
// It exposes a [Transport] function that returns an [http.RoundTripper] backed
// by [github.com/quic-go/quic-go/http3] and a [WithHTTP3] option that wires
// the transport into a relay client.
//
// # Basic usage
//
//	client := relay.New(
//	    http3ext.WithHTTP3(),
//	)
//
// # Custom TLS and timeouts
//
//	cfg := &http3ext.Config{
//	    TLSConfig:       myTLSConfig,
//	    MaxIdleConns:    50,
//	    IdleConnTimeout: 90 * time.Second,
//	}
//	client := relay.New(
//	    relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
//	        return cfg.Transport()
//	    }),
//	)
package http3ext

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/jhonsferg/relay"
	quichttp3 "github.com/quic-go/quic-go/http3"
)

// Config holds configuration for the HTTP/3 QUIC transport.
type Config struct {
	// TLSConfig is the TLS configuration for QUIC connections. When nil a
	// default config with TLS 1.3 (the minimum for QUIC) is used.
	TLSConfig *tls.Config

	// MaxIdleConns is advisory only; the QUIC transport does not have the same
	// pooling model as HTTP/1.1. It is stored here for documentation purposes
	// and may be used by future extensions.
	MaxIdleConns int

	// IdleConnTimeout is advisory for now; stored for forward-compatibility.
	IdleConnTimeout time.Duration
}

// Transport returns an [http.RoundTripper] that uses HTTP/3 over QUIC for all
// requests. The returned transport is safe for concurrent use by multiple
// goroutines.
//
// Callers should store the returned transport and call its Close method when
// the client is no longer needed to release the underlying UDP socket. The
// transport satisfies both [http.RoundTripper] and [io.Closer].
func (c *Config) Transport() http.RoundTripper {
	tlsCfg := c.TLSConfig
	if tlsCfg == nil {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS13} //nolint:gosec
	}
	return &quichttp3.Transport{
		TLSClientConfig: tlsCfg,
	}
}

// Transport returns a default HTTP/3 transport. It is a convenience wrapper
// equivalent to (&Config{}).Transport().
func Transport() http.RoundTripper {
	return (&Config{}).Transport()
}

// WithHTTP3 returns a relay [relay.Option] that replaces the default transport
// with an HTTP/3 QUIC transport built from default settings. Use
// [WithHTTP3Config] for custom TLS or connection pool parameters.
func WithHTTP3() relay.Option {
	return relay.WithTransportMiddleware(func(_ http.RoundTripper) http.RoundTripper {
		return Transport()
	})
}

// WithHTTP3Config returns a relay [relay.Option] that replaces the default
// transport with an HTTP/3 QUIC transport built from cfg.
func WithHTTP3Config(cfg *Config) relay.Option {
	return relay.WithTransportMiddleware(func(_ http.RoundTripper) http.RoundTripper {
		return cfg.Transport()
	})
}
