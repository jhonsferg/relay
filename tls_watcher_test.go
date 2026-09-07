package relay

import (
	"context"
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

// TestCertWatcher_NonPositiveIntervalFails guards against run()'s
// time.NewTicker(w.interval) panicking (crashing the process - the
// background goroutine is unrecovered) for interval <= 0, a plausible
// misconfiguration (e.g. a zero-value config field left unset) that
// newCertWatcher previously never validated.
func TestCertWatcher_NonPositiveIntervalFails(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir, "zero-interval")

	for _, interval := range []time.Duration{0, -time.Second} {
		_, err := newCertWatcher(certFile, keyFile, interval)
		if err == nil {
			t.Errorf("expected error for interval=%v, got nil", interval)
		}
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

// TestClient_Shutdown_StopsOwnedCertWatcher guards against a regression
// where the background reload goroutine spawned by WithDynamicTLSCert was
// never stopped by Client.Shutdown, leaking a goroutine + time.Ticker for
// the life of the process on every client created with this option -
// Shutdown already had a working pattern for exactly this (bgCancel stops
// the health-check goroutine) but the cert watcher was never wired into it.
func TestClient_Shutdown_StopsOwnedCertWatcher(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir, "shutdown-owned")

	c := New(WithDynamicTLSCert(certFile, keyFile, time.Hour))
	w := c.config.CertWatcher
	if w == nil {
		t.Fatal("expected CertWatcher to be set")
	}

	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case <-w.stopCh:
		// Stopped, as expected.
	default:
		t.Error("expected Shutdown to stop the CertWatcher it created (stopCh should be closed)")
	}
}

// TestClient_Shutdown_DoesNotStopCallerManagedCertWatcher is the
// WithCertWatcher counterpart: a caller-supplied watcher may be shared
// across multiple clients, so Shutdown on one client must not stop it.
func TestClient_Shutdown_DoesNotStopCallerManagedCertWatcher(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir, "shutdown-caller-managed")

	w, err := newCertWatcher(certFile, keyFile, time.Hour)
	if err != nil {
		t.Fatalf("newCertWatcher: %v", err)
	}
	defer w.Stop()

	c := New(WithCertWatcher(w))
	if err := c.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case <-w.stopCh:
		t.Error("Shutdown must not stop a caller-managed (WithCertWatcher) watcher - it may be shared")
	default:
		// Still running, as expected.
	}
}

// TestClient_With_DoesNotShareCertWatcherOwnership guards against a bug
// where (*Client).With's clone of Config carries ownsCertWatcher=true (and
// the same *CertWatcher pointer) straight to the child, breaking
// WithDynamicTLSCert's documented invariant that the watcher is "exclusively
// owned by this client (nothing else can reach it)" - see that doc comment.
// After With, both the parent and the child reach the same watcher and both
// have ownsCertWatcher=true, so Shutdown on either one stops the watcher
// out from under the other, still-live client, silently killing its
// certificate rotation with no error or indication anything went wrong.
func TestClient_With_DoesNotShareCertWatcherOwnership(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir, "with-owned")

	parent := New(WithDynamicTLSCert(certFile, keyFile, time.Hour))
	w := parent.config.CertWatcher
	if w == nil {
		t.Fatal("expected CertWatcher to be set")
	}

	child := parent.With(WithTimeout(5 * time.Second))

	if err := child.Shutdown(context.Background()); err != nil {
		t.Fatalf("child.Shutdown: %v", err)
	}

	select {
	case <-w.stopCh:
		t.Error("child.Shutdown stopped the parent's still-live CertWatcher - With must not propagate exclusive ownership to a second client")
	default:
		// Still running, as expected - the parent is unaffected by the child's Shutdown.
	}
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
