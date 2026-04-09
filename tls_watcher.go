package relay

import (
	"crypto/tls"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// CertWatcher hot-reloads a TLS certificate/key pair from disk at a fixed
// interval without restarting the process. Use [WithDynamicTLSCert] to create
// one and attach it to a [Client], or [WithCertWatcher] to manage the lifecycle
// yourself.
type CertWatcher struct {
	certFile string
	keyFile  string
	interval time.Duration
	cert     atomic.Pointer[tls.Certificate]
	stopOnce sync.Once
	stopCh   chan struct{}
}

// newCertWatcher creates a CertWatcher that performs an initial load and then
// reloads on the given interval. Returns an error if the initial load fails.
func newCertWatcher(certFile, keyFile string, interval time.Duration) (*CertWatcher, error) {
	w := &CertWatcher{
		certFile: certFile,
		keyFile:  keyFile,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
	if err := w.reload(); err != nil {
		return nil, err
	}
	go w.run()
	return w, nil
}

// reload reads the certificate and key files and updates the atomic pointer.
func (w *CertWatcher) reload() error {
	cert, err := tls.LoadX509KeyPair(w.certFile, w.keyFile)
	if err != nil {
		return fmt.Errorf("tls_watcher: load cert %s/%s: %w", w.certFile, w.keyFile, err)
	}
	w.cert.Store(&cert)
	return nil
}

// run is the background loop that reloads the cert on the configured interval.
func (w *CertWatcher) run() {
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			_ = w.reload() // ignore reload errors; keep serving last good cert
		case <-w.stopCh:
			return
		}
	}
}

// Stop halts the background reload goroutine. It is safe to call more than once
// and from multiple goroutines concurrently; only the first call has any effect.
func (w *CertWatcher) Stop() {
	w.stopOnce.Do(func() {
		close(w.stopCh)
	})
}

// getClientCertificate implements tls.Config.GetClientCertificate. It returns
// the most recently loaded certificate regardless of the CertificateRequest.
func (w *CertWatcher) getClientCertificate(_ *tls.CertificateRequestInfo) (*tls.Certificate, error) {
	cert := w.cert.Load()
	if cert == nil {
		return nil, fmt.Errorf("tls_watcher: no certificate loaded")
	}
	return cert, nil
}

// WithDynamicTLSCert enables hot-reloading of the TLS client certificate.
// The certificate and key are loaded immediately; an error is silently ignored
// at construction time - callers that need hard failure should use
// [WithCertWatcher] instead, where they control initialisation.
//
// The interval controls how often the files are re-read. The returned
// [CertWatcher] is stored in [Config] so callers can stop it via
// [Config.CertWatcher].Stop() when done.
func WithDynamicTLSCert(certFile, keyFile string, interval time.Duration) Option {
	return func(c *Config) {
		w, err := newCertWatcher(certFile, keyFile, interval)
		if err != nil {
			return // initial load failed; leave TLS config unchanged
		}
		c.CertWatcher = w
		if c.TLSConfig == nil {
			c.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12} //nolint:gosec
		}
		c.TLSConfig.GetClientCertificate = w.getClientCertificate
	}
}

// WithCertWatcher attaches a pre-constructed [CertWatcher] to the client. The
// watcher's GetClientCertificate hook is installed on the TLS config. Callers
// are responsible for starting and stopping the watcher's lifecycle.
func WithCertWatcher(w *CertWatcher) Option {
	return func(c *Config) {
		if w == nil {
			return
		}
		c.CertWatcher = w
		if c.TLSConfig == nil {
			c.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12} //nolint:gosec
		}
		c.TLSConfig.GetClientCertificate = w.getClientCertificate
	}
}
