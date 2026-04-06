package relay_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
)

// generateSelfSignedCert creates an ephemeral self-signed certificate and key
// in PEM format for use in TLS tests.
func generateSelfSignedCert(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "relay-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
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

func TestWithClientCertPEM(t *testing.T) {
	certPEM, keyPEM := generateSelfSignedCert(t)

	// Build a TLS server that requires client authentication.
	serverCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("server X509KeyPair: %v", err)
	}
	clientPool := x509.NewCertPool()
	clientPool.AppendCertsFromPEM(certPEM)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAnyClientCert,
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	// Build a relay client that presents the client cert and trusts the server's
	// self-signed certificate via WithRootCA (avoids option-ordering issue with
	// WithTLSConfig replacing the Certificates slice).
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithRootCA(certPEM),
		relay.WithClientCertPEM(certPEM, keyPEM),
	)

	resp, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestWithClientCertPEM_InvalidPEM(t *testing.T) {
	// Invalid PEM should not panic; the option is silently ignored.
	client := relay.New(
		relay.WithClientCertPEM([]byte("not-a-cert"), []byte("not-a-key")),
	)
	if client == nil {
		t.Fatal("client should not be nil with invalid cert PEM")
	}
}

func TestWithRootCA(t *testing.T) {
	certPEM, keyPEM := generateSelfSignedCert(t)

	serverCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("server X509KeyPair: %v", err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{serverCert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithRootCA(certPEM),
	)

	resp, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("Execute with custom CA: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}
