package relay

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// generateSelfSignedCert creates a minimal self-signed certificate with
// 127.0.0.1 as an IP SAN (required for connecting to httptest.Server).
// Returns the DER-encoded certificate bytes, the PEM block, and the private key.
func generateSelfSignedCert(t *testing.T) (derBytes []byte, certPEM []byte, key *rsa.PrivateKey) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("x509.CreateCertificate: %v", err)
	}

	certPEMBlock := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return der, certPEMBlock, privateKey
}

// pin computes sha256/BASE64 for the given DER certificate bytes.
func pin(derBytes []byte) string {
	digest := sha256.Sum256(derBytes)
	return "sha256/" + base64.StdEncoding.EncodeToString(digest[:])
}

func TestTLSPinning_CorrectPinSucceeds(t *testing.T) {
	t.Parallel()

	der, certPEM, key := generateSelfSignedCert(t)

	// Build TLS config for the test server using our generated cert.
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("tls.X509KeyPair: %v", err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}, MinVersion: tls.VersionTLS12} //nolint:gosec
	srv.StartTLS()
	defer srv.Close()

	correctPin := pin(der)

	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithCertificatePinning([]string{correctPin}),
		// Use the server's cert as root CA so standard chain validation passes.
		WithTLSConfig(&tls.Config{
			RootCAs:    mustCertPool(t, certPEM),
			MinVersion: tls.VersionTLS12,
		}),
	)

	resp, err := c.Execute(c.Get(srv.URL + "/"))
	if err != nil {
		t.Fatalf("expected success with correct pin, got: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestTLSPinning_WrongPinFails(t *testing.T) {
	t.Parallel()

	_, certPEM, key := generateSelfSignedCert(t)

	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("tls.X509KeyPair: %v", err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{tlsCert}, MinVersion: tls.VersionTLS12} //nolint:gosec
	srv.StartTLS()
	defer srv.Close()

	// Use a wrong pin.
	wrongPin := "sha256/" + base64.StdEncoding.EncodeToString(make([]byte, 32))

	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithCertificatePinning([]string{wrongPin}),
		WithTLSConfig(&tls.Config{
			RootCAs:    mustCertPool(t, certPEM),
			MinVersion: tls.VersionTLS12,
		}),
	)

	var execErr error
	_, execErr = c.Execute(c.Get(srv.URL + "/"))
	if execErr == nil {
		t.Fatal("expected error with wrong pin, got nil")
	}
	if !errors.Is(execErr, ErrCertificatePinMismatch) {
		t.Errorf("expected ErrCertificatePinMismatch, got %v", execErr)
	}
}

func TestParsePins_ValidFormat(t *testing.T) {
	t.Parallel()

	// Create a valid base64 string.
	raw := make([]byte, 32)
	b64 := base64.StdEncoding.EncodeToString(raw)

	pins := []string{
		"sha256/" + b64,
		b64, // without prefix
	}

	set, err := parsePins(pins)
	if err != nil {
		t.Fatalf("parsePins returned error: %v", err)
	}
	if len(set) != 1 {
		// Both forms resolve to the same base64, so there's only 1 unique entry.
		t.Errorf("expected 1 unique pin, got %d", len(set))
	}
	if _, ok := set[b64]; !ok {
		t.Errorf("expected %q in pin set", b64)
	}
}

func TestParsePins_InvalidBase64ReturnsError(t *testing.T) {
	t.Parallel()

	_, err := parsePins([]string{"sha256/!!!notbase64!!!"})
	if err == nil {
		t.Error("expected error for invalid base64 pin")
	}
}

func TestParsePins_EmptyInput(t *testing.T) {
	t.Parallel()

	set, err := parsePins([]string{})
	if err != nil {
		t.Fatalf("parsePins([]) returned error: %v", err)
	}
	if len(set) != 0 {
		t.Errorf("expected empty set, got %v", set)
	}
}

func TestBuildPinVerifier_MatchingCertPasses(t *testing.T) {
	t.Parallel()

	der, _, _ := generateSelfSignedCert(t)
	correctPin := pin(der)

	verifier, err := buildPinVerifier([]string{correctPin})
	if err != nil {
		t.Fatalf("buildPinVerifier: %v", err)
	}

	// Pass the raw cert to the verifier.
	if err := verifier([][]byte{der}, nil); err != nil {
		t.Errorf("expected nil error for matching cert, got %v", err)
	}
}

func TestBuildPinVerifier_NonMatchingCertFails(t *testing.T) {
	t.Parallel()

	der, _, _ := generateSelfSignedCert(t)
	otherDer, _, _ := generateSelfSignedCert(t)

	correctPin := pin(otherDer) // pin for a different cert

	verifier, err := buildPinVerifier([]string{correctPin})
	if err != nil {
		t.Fatalf("buildPinVerifier: %v", err)
	}

	err = verifier([][]byte{der}, nil)
	if !errors.Is(err, ErrCertificatePinMismatch) {
		t.Errorf("expected ErrCertificatePinMismatch, got %v", err)
	}
}

func TestBuildTLSConfigWithPinning_AttachesVerifier(t *testing.T) {
	t.Parallel()

	der, _, _ := generateSelfSignedCert(t)
	correctPin := pin(der)

	cfg, err := buildTLSConfigWithPinning(nil, []string{correctPin})
	if err != nil {
		t.Fatalf("buildTLSConfigWithPinning: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil TLS config")
	}
	if cfg.VerifyPeerCertificate == nil {
		t.Error("expected VerifyPeerCertificate to be set")
	}
	// The verifier should pass for the correct cert.
	if err := cfg.VerifyPeerCertificate([][]byte{der}, nil); err != nil {
		t.Errorf("verifier rejected matching cert: %v", err)
	}
}

// mustCertPool creates a *x509.CertPool from PEM-encoded certificate bytes.
func mustCertPool(t *testing.T, certPEM []byte) *x509.CertPool {
	t.Helper()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("failed to append certificate to pool")
	}
	return pool
}
