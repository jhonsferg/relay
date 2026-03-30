package relay

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// ErrCertificatePinMismatch is returned when the server's certificate chain
// does not contain any certificate whose SHA-256 fingerprint matches the
// configured pins.
var ErrCertificatePinMismatch = errors.New("relay: TLS certificate pin mismatch")

// parsePins normalises pin strings of the form "sha256/BASE64==" or just raw
// base64, returning the set of expected fingerprints.
func parsePins(pins []string) (map[string]struct{}, error) {
	set := make(map[string]struct{}, len(pins))
	for _, pin := range pins {
		p := pin
		p = strings.TrimPrefix(p, "sha256/")
		// Verify the base64 is valid.
		if _, err := base64.StdEncoding.DecodeString(p); err != nil {
			return nil, fmt.Errorf("relay: invalid certificate pin %q: %w", pin, err)
		}
		set[p] = struct{}{}
	}
	return set, nil
}

// buildPinVerifier returns a tls.VerifyPeerCertificate function that rejects
// connections whose certificate chain does not contain a matching SHA-256 pin.
func buildPinVerifier(pins []string) (func([][]byte, [][]*x509.Certificate) error, error) {
	pinSet, err := parsePins(pins)
	if err != nil {
		return nil, err
	}

	return func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
		for _, rawCert := range rawCerts {
			digest := sha256.Sum256(rawCert)
			encoded := base64.StdEncoding.EncodeToString(digest[:])
			if _, ok := pinSet[encoded]; ok {
				return nil
			}
		}
		return ErrCertificatePinMismatch
	}, nil
}

// buildTLSConfigWithPinning clones baseTLS (or creates a new config) and
// attaches the pin verifier.
func buildTLSConfigWithPinning(baseTLS *tls.Config, pins []string) (*tls.Config, error) {
	verifier, err := buildPinVerifier(pins)
	if err != nil {
		return nil, err
	}

	var tlsCfg *tls.Config
	if baseTLS != nil {
		tlsCfg = baseTLS.Clone()
	} else {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12} //nolint:gosec
	}

	tlsCfg.VerifyPeerCertificate = verifier
	// InsecureSkipVerify must be false (default) so the standard chain
	// validation still runs; our verifier adds the pin check on top.
	return tlsCfg, nil
}
