package http3ext_test

import (
	"crypto/tls"
	"net/http"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
	http3ext "github.com/jhonsferg/relay/ext/http3"
)

func TestTransport_NotNil(t *testing.T) {
	rt := http3ext.Transport()
	if rt == nil {
		t.Fatal("Transport() returned nil")
	}
}

func TestTransport_ImplementsRoundTripper(t *testing.T) {
	var _ http.RoundTripper = http3ext.Transport()
}

func TestConfig_Transport_DefaultTLS(t *testing.T) {
	cfg := &http3ext.Config{}
	rt := cfg.Transport()
	if rt == nil {
		t.Fatal("Config.Transport() returned nil")
	}
}

func TestConfig_Transport_CustomTLS(t *testing.T) {
	tlsCfg := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, //nolint:gosec // test only
	}
	cfg := &http3ext.Config{TLSConfig: tlsCfg}
	rt := cfg.Transport()
	if rt == nil {
		t.Fatal("Config.Transport() with custom TLS returned nil")
	}
}

func TestConfig_Fields(t *testing.T) {
	cfg := &http3ext.Config{
		MaxIdleConns:    50,
		IdleConnTimeout: 90 * time.Second,
	}
	if cfg.MaxIdleConns != 50 {
		t.Errorf("expected MaxIdleConns=50, got %d", cfg.MaxIdleConns)
	}
	if cfg.IdleConnTimeout != 90*time.Second {
		t.Errorf("expected IdleConnTimeout=90s, got %v", cfg.IdleConnTimeout)
	}
}

func TestWithHTTP3_AppliesTransport(t *testing.T) {
	// Verify that WithHTTP3() can be passed to relay.New without panicking.
	c := relay.New(
		http3ext.WithHTTP3(),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)
	if c == nil {
		t.Fatal("relay.New with WithHTTP3 returned nil")
	}
}

func TestWithHTTP3Config_AppliesTransport(t *testing.T) {
	cfg := &http3ext.Config{
		TLSConfig: &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // test only
		},
	}
	c := relay.New(
		http3ext.WithHTTP3Config(cfg),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)
	if c == nil {
		t.Fatal("relay.New with WithHTTP3Config returned nil")
	}
}
