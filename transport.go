package relay

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"time"
)

// overrideDialer is a [net.Dialer] wrapper that substitutes specific hostnames
// with fixed IP addresses before dialing, effectively bypassing DNS resolution
// for those hosts. See [WithDNSOverride].
type overrideDialer struct {
	// base is the underlying dialer used for the actual TCP connection.
	base *net.Dialer

	// hosts maps hostname → IP address. Only the hostname portion of the
	// target address is compared; the port is preserved as-is.
	hosts map[string]string
}

// DialContext resolves the target hostname against the override map. If a
// match is found the fixed IP replaces the hostname before dialing; otherwise
// the address is passed to the base dialer unchanged.
func (d *overrideDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if host, port, err := net.SplitHostPort(addr); err == nil {
		if ip, ok := d.hosts[host]; ok {
			addr = net.JoinHostPort(ip, port)
		}
	}
	return d.base.DialContext(ctx, network, addr)
}

// buildTransport constructs an *http.Transport from the given [Config].
//
// Notable characteristics:
//   - ForceAttemptHTTP2 is always true so HTTP/2 is negotiated when the server
//     supports it, unless a custom TLS config disables ALPN.
//   - The minimum TLS version is TLS 1.2 when no custom [tls.Config] is provided.
//   - Read and write buffer sizes are set to 64 KiB for throughput efficiency.
//   - Proxy is sourced from the environment unless [Config.ProxyURL] is set.
//   - DNS overrides are applied by wrapping the dialer with [overrideDialer].
func buildTransport(cfg *Config) http.RoundTripper {
	// Prefer a custom dialer when provided; otherwise build one from timeouts.
	dialer := cfg.CustomDialer
	if dialer == nil {
		dialer = &net.Dialer{
			Timeout:   cfg.DialTimeout,
			KeepAlive: cfg.DialKeepAlive,
		}
	}

	dialFn := dialer.DialContext
	if len(cfg.DNSOverrides) > 0 {
		od := &overrideDialer{base: dialer, hosts: cfg.DNSOverrides}
		dialFn = od.DialContext
	}

	tlsCfg := cfg.TLSConfig
	if tlsCfg == nil {
		// Enforce TLS 1.2 minimum when no explicit config is provided.
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	proxyFunc := http.ProxyFromEnvironment
	if cfg.ProxyURL != "" {
		if proxyURL, err := url.Parse(cfg.ProxyURL); err == nil {
			proxyFunc = http.ProxyURL(proxyURL)
		}
	}

	return &http.Transport{
		DialContext:           dialFn,
		Proxy:                 proxyFunc,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
		MaxConnsPerHost:       cfg.MaxConnsPerHost,
		IdleConnTimeout:       cfg.IdleConnTimeout,
		TLSHandshakeTimeout:   cfg.TLSHandshakeTimeout,
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
		TLSClientConfig:       tlsCfg,
		DisableCompression:    cfg.DisableCompression,
		ForceAttemptHTTP2:     true,
		ExpectContinueTimeout: 1 * time.Second,
		WriteBufferSize:       64 * 1024,
		ReadBufferSize:        64 * 1024,
	}
}
