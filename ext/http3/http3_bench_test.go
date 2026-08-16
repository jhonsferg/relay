package http3ext_test

// Benchmarks the real in-process QUIC/HTTP3 request round trip. Unlike
// http3_functional_test.go's TestHTTP3_RealRequestRoundTrip (a single
// request), this reuses one relay.Client - and therefore one underlying QUIC
// connection - across the whole b.N loop, since a full handshake per
// iteration would dominate the measurement and defeat the point of
// benchmarking the transport's steady-state request path.

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
	"testing"
	"time"

	quichttp3 "github.com/quic-go/quic-go/http3"

	"github.com/jhonsferg/relay"
	http3ext "github.com/jhonsferg/relay/ext/http3"
)

func benchGenerateSelfSignedCert(b *testing.B) (certPEM, keyPEM []byte) {
	b.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		b.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "relay-http3-bench"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		b.Fatalf("create cert: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		b.Fatalf("marshal key: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func benchStartHTTP3Server(b *testing.B, handler http.Handler) string {
	b.Helper()

	certPEM, keyPEM := benchGenerateSelfSignedCert(b)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		b.Fatalf("X509KeyPair: %v", err)
	}

	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		b.Fatalf("ListenUDP: %v", err)
	}

	server := &quichttp3.Server{
		TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}},
		Handler:   handler,
	}
	b.Cleanup(func() {
		_ = server.Close()
		_ = udpConn.Close()
	})

	go func() { _ = server.Serve(udpConn) }()

	return udpConn.LocalAddr().String()
}

// BenchmarkHTTP3_RoundTrip measures a full request/response cycle over a
// single, already-established QUIC connection - the steady-state hot path
// once the handshake cost has been paid.
func BenchmarkHTTP3_RoundTrip(b *testing.B) {
	addr := benchStartHTTP3Server(b, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	cfg := &http3ext.Config{
		TLSConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed bench cert
	}
	client := relay.New(
		http3ext.WithHTTP3Config(cfg),
		relay.WithBaseURL(fmt.Sprintf("https://%s", addr)),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(10*time.Second),
	)

	// Warm up the connection (handshake) before timing.
	if _, err := client.Execute(client.Get("/hello")); err != nil {
		b.Fatalf("warmup Execute: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Get("/hello"))
		if err != nil {
			b.Fatalf("Execute: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("StatusCode = %d, want 200", resp.StatusCode)
		}
	}
}
