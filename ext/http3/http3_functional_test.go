package http3ext_test

// This file adds the first functional HTTP/3 request/response test for this
// package - the existing tests in http3_test.go only ever assert that
// Transport()/WithHTTP3() don't panic and return non-nil values; no real
// QUIC/HTTP3 request was ever made. This spins up a real in-process
// quic-go/http3 server (self-signed cert, real UDP socket) and drives a real
// relay client through WithHTTP3Config against it.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	quichttp3 "github.com/quic-go/quic-go/http3"

	"github.com/jhonsferg/relay"
	http3ext "github.com/jhonsferg/relay/ext/http3"
)

// generateSelfSignedCert creates an ephemeral self-signed certificate and key
// in PEM format, valid for 127.0.0.1, for use in this test only.
func generateSelfSignedCert(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "relay-http3-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

// startHTTP3TestServer starts a real quic-go/http3 server on an ephemeral
// local UDP port serving handler, and returns its address ("127.0.0.1:port").
// The server and its UDP socket are closed in t.Cleanup.
func startHTTP3TestServer(t *testing.T, handler http.Handler) string {
	t.Helper()

	certPEM, keyPEM := generateSelfSignedCert(t)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}

	server := &quichttp3.Server{
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
		Handler:   handler,
	}
	t.Cleanup(func() {
		_ = server.Close()
		_ = udpConn.Close()
	})

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(udpConn) }()
	t.Cleanup(func() {
		select {
		case err := <-serveErr:
			if err != nil && err != http.ErrServerClosed {
				t.Logf("http3 server.Serve: %v", err)
			}
		case <-time.After(time.Second):
		}
	})

	return udpConn.LocalAddr().String()
}

func TestHTTP3_RealRequestRoundTrip(t *testing.T) {
	// Guarded by mu: the handler runs on quic-go's own server goroutine,
	// which - unlike net/http/httptest's Transport - gives the race
	// detector no visible happens-before edge back to the goroutine that
	// reads gotPath below, even though client.Execute has already returned
	// the full response by the time that read happens.
	var mu sync.Mutex
	var gotPath string
	addr := startHTTP3TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	cfg := &http3ext.Config{
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed test cert
	}
	client := relay.New(
		http3ext.WithHTTP3Config(cfg),
		relay.WithBaseURL(fmt.Sprintf("https://%s", addr)),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(10*time.Second),
	)

	resp, err := client.Execute(client.Get("/hello"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if body := resp.String(); body != `{"ok":true}` {
		t.Errorf("body = %q, want {\"ok\":true}", body)
	}
	mu.Lock()
	got := gotPath
	mu.Unlock()
	if got != "/hello" {
		t.Errorf("server saw path %q, want /hello", got)
	}
	if resp.Raw().ProtoMajor != 3 {
		t.Errorf("ProtoMajor = %d, want 3 (real HTTP/3 exchange)", resp.Raw().ProtoMajor)
	}
}
