package relay

import (
	"crypto/tls"
	"crypto/x509"
)

// WithClientCert configures the client to present a static TLS client
// certificate for mutual TLS (mTLS) authentication. The certificate and key
// are loaded once at construction time; use [WithDynamicTLSCert] instead when
// hot-reloading is required.
//
// Example:
//
//	client := relay.New(
//	    relay.WithClientCert("/certs/client.crt", "/certs/client.key"),
//	)
func WithClientCert(certFile, keyFile string) Option {
	return func(c *Config) {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return // leave TLS config unchanged on load error
		}
		if c.TLSConfig == nil {
			c.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12} //nolint:gosec
		}
		c.TLSConfig.Certificates = append(c.TLSConfig.Certificates, cert)
	}
}

// WithClientCertPEM configures the client to present a TLS client certificate
// loaded from PEM-encoded bytes. Use this when certificates are sourced from
// environment variables, secret managers (Vault, AWS Secrets Manager), or
// in-memory configuration rather than disk files.
//
// Example:
//
//	client := relay.New(
//	    relay.WithClientCertPEM(certPEM, keyPEM),
//	)
func WithClientCertPEM(certPEM, keyPEM []byte) Option {
	return func(c *Config) {
		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return // leave TLS config unchanged on parse error
		}
		if c.TLSConfig == nil {
			c.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12} //nolint:gosec
		}
		c.TLSConfig.Certificates = append(c.TLSConfig.Certificates, cert)
	}
}

// WithRootCA adds a PEM-encoded CA certificate to the client's TLS trust
// store. Use this for private PKI environments where the server certificate is
// signed by an internal CA not present in the system certificate pool.
//
// Multiple calls append additional CAs without replacing earlier ones.
//
// Example:
//
//	client := relay.New(
//	    relay.WithRootCA(internalCAPEM),
//	)
func WithRootCA(caPEM []byte) Option {
	return func(c *Config) {
		if c.TLSConfig == nil {
			c.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12} //nolint:gosec
		}
		if c.TLSConfig.RootCAs == nil {
			pool, err := x509.SystemCertPool()
			if err != nil {
				pool = x509.NewCertPool()
			}
			c.TLSConfig.RootCAs = pool
		}
		c.TLSConfig.RootCAs.AppendCertsFromPEM(caPEM)
	}
}
