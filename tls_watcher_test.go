package relay

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
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTLSKeyPair generates a self-signed cert/key pair, writes them to files
// under dir, and returns their paths.
func writeTLSKeyPair(t *testing.T, dir, prefix string) (certFile, keyFile string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}

	certPath := filepath.Join(dir, prefix+"_cert.pem")
	keyPath := filepath.Join(dir, prefix+"_key.pem")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if writeErr := os.WriteFile(certPath, certPEM, 0600); writeErr != nil {
		t.Fatalf("WriteFile cert: %v", writeErr)
	}

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatalf("WriteFile key: %v", err)
	}

	return certPath, keyPath
}

func TestCertWatcher_InitialLoad(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir, "initial")

	w, err := newCertWatcher(certFile, keyFile, time.Minute)
	if err != nil {
		t.Fatalf("newCertWatcher: %v", err)
	}
	defer w.Stop()

	cert, err := w.getClientCertificate(nil)
	if err != nil {
		t.Fatalf("getClientCertificate: %v", err)
	}
	if cert == nil {
		t.Fatal("expected non-nil certificate")
	}
}

func TestCertWatcher_Stop_Idempotent(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir, "stop")

	w, err := newCertWatcher(certFile, keyFile, time.Minute)
	if err != nil {
		t.Fatalf("newCertWatcher: %v", err)
	}
	// Calling Stop twice should not panic.
	w.Stop()
	w.Stop()
}

func TestCertWatcher_Rotation(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir, "rotate")

	w, err := newCertWatcher(certFile, keyFile, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("newCertWatcher: %v", err)
	}
	defer w.Stop()

	// Record the initial certificate leaf.
	first, err := w.getClientCertificate(nil)
	if err != nil {
		t.Fatalf("initial cert: %v", err)
	}
	firstLeaf := first.Leaf

	// Write a new certificate to the same files.
	writeTLSKeyPair(t, dir, "rotate") // overwrites rotate_cert.pem / rotate_key.pem

	// Wait for the watcher to reload (interval = 20ms; allow up to 500ms).
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		time.Sleep(30 * time.Millisecond)
		second, getErr := w.getClientCertificate(nil)
		if getErr != nil {
			continue
		}
		if second.Leaf != firstLeaf {
			// Pointer changed - reload happened.
			return
		}
	}
	// If the leaf pointer did not change, the cert bytes must differ because
	// a new private key was generated. Parse and compare serial numbers.
	second, _ := w.getClientCertificate(nil)
	if second == nil {
		t.Fatal("no cert after rotation wait")
	}
	// A new key pair must produce a different DER encoding.
	if len(second.Certificate) > 0 && len(first.Certificate) > 0 {
		if string(second.Certificate[0]) == string(first.Certificate[0]) {
			t.Error("expected cert bytes to change after rotation")
		}
	}
}

func TestCertWatcher_InvalidFilesFail(t *testing.T) {
	_, err := newCertWatcher("/nonexistent/cert.pem", "/nonexistent/key.pem", time.Minute)
	if err == nil {
		t.Fatal("expected error for missing cert files")
	}
}

func TestWithDynamicTLSCert(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir, "dynamic")

	c := New(WithDynamicTLSCert(certFile, keyFile, time.Minute))
	if c.config.CertWatcher == nil {
		t.Fatal("expected CertWatcher to be set on Config")
	}
	if c.config.TLSConfig == nil {
		t.Fatal("expected TLSConfig to be set")
	}
	if c.config.TLSConfig.GetClientCertificate == nil {
		t.Fatal("expected GetClientCertificate hook to be set")
	}
	c.config.CertWatcher.Stop()
}

func TestWithDynamicTLSCert_InvalidIgnored(t *testing.T) {
	// Invalid paths should not panic; the option is a no-op.
	c := New(WithDynamicTLSCert("/bad/cert", "/bad/key", time.Minute))
	if c.config.CertWatcher != nil {
		t.Error("expected CertWatcher to be nil on bad paths")
	}
}

func TestWithCertWatcher(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir, "watcher")

	w, err := newCertWatcher(certFile, keyFile, time.Minute)
	if err != nil {
		t.Fatalf("newCertWatcher: %v", err)
	}
	defer w.Stop()

	c := New(WithCertWatcher(w))
	if c.config.CertWatcher != w {
		t.Fatal("expected CertWatcher to be stored in Config")
	}
	if c.config.TLSConfig == nil || c.config.TLSConfig.GetClientCertificate == nil {
		t.Fatal("expected GetClientCertificate to be installed")
	}
}

func TestWithCertWatcher_NilNoOp(t *testing.T) {
	c := New(WithCertWatcher(nil))
	if c.config.CertWatcher != nil {
		t.Error("nil CertWatcher should not be stored")
	}
}

func TestWithDynamicTLSCert_PreservesExistingTLSConfig(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir, "preserve")

	existingCfg := &tls.Config{MinVersion: tls.VersionTLS13}
	c := New(WithTLSConfig(existingCfg), WithDynamicTLSCert(certFile, keyFile, time.Minute))
	if c.config.TLSConfig == nil {
		t.Fatal("TLSConfig should not be nil")
	}
	if c.config.TLSConfig.GetClientCertificate == nil {
		t.Fatal("GetClientCertificate should be set on existing TLSConfig")
	}
	c.config.CertWatcher.Stop()
}
