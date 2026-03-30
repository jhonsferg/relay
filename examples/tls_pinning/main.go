// Package main demonstrates relay's TLS certificate pinning feature.
// Certificate pinning rejects connections whose server certificate does not
// match a known SHA-256 fingerprint, protecting against CA compromise and
// MITM attacks even when the certificate is valid.
package main

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"

	relay "github.com/jhonsferg/relay"
)

func main() {
	// -------------------------------------------------------------------------
	// 1. Start a TLS test server and extract its certificate fingerprint.
	//
	// In production you obtain the pin from the service operator (e.g. pinned
	// in your app binary or fetched from a trusted configuration store).
	// -------------------------------------------------------------------------
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"ok","pinned":true}`)
	}))
	defer srv.Close()

	// Derive the SHA-256 fingerprint of the server's leaf certificate.
	cert := srv.TLS.Certificates[0].Certificate[0]
	digest := sha256.Sum256(cert)
	correctPin := "sha256/" + base64.StdEncoding.EncodeToString(digest[:])
	wrongPin := "sha256/" + base64.StdEncoding.EncodeToString(make([]byte, 32)) // all zeros

	fmt.Printf("Server certificate pin: %s\n\n", correctPin)

	// -------------------------------------------------------------------------
	// 2. Client with the correct pin - request succeeds.
	// -------------------------------------------------------------------------
	fmt.Println("=== Correct pin → request succeeds ===")
	pinnedClient := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithCertificatePinning([]string{correctPin}),
		// The test server uses a self-signed cert - skip normal chain validation
		// so only our pin check matters. In production you do NOT skip verify.
		relay.WithTLSConfig(&tls.Config{InsecureSkipVerify: true}), //nolint:gosec
	)

	resp, err := pinnedClient.Execute(pinnedClient.Get("/api"))
	if err != nil {
		log.Fatalf("unexpected error with correct pin: %v", err)
	}
	fmt.Printf("  status: %d\n  body:   %s\n\n", resp.StatusCode, resp.String())

	// -------------------------------------------------------------------------
	// 3. Client with a wrong pin - connection is rejected.
	// -------------------------------------------------------------------------
	fmt.Println("=== Wrong pin → connection rejected ===")
	wrongClient := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithCertificatePinning([]string{wrongPin}),
		relay.WithTLSConfig(&tls.Config{InsecureSkipVerify: true}), //nolint:gosec
		relay.WithDisableRetry(),
	)

	_, err = wrongClient.Execute(wrongClient.Get("/api"))
	if err == nil {
		log.Fatal("expected error with wrong pin, got nil")
	}
	if errors.Is(err, relay.ErrCertificatePinMismatch) {
		fmt.Printf("  got expected error: %v\n\n", err)
	} else {
		// The pin verifier error is wrapped inside a TLS handshake error.
		fmt.Printf("  got expected TLS error: %v\n\n", err)
	}

	// -------------------------------------------------------------------------
	// 4. Multiple pins - allows rolling key rotation.
	//
	// Pin both the current and next certificate so that rotation can happen
	// on the server without taking clients down. Clients accept either pin.
	// -------------------------------------------------------------------------
	fmt.Println("=== Multiple pins (key rotation support) ===")
	multiPinClient := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithCertificatePinning([]string{wrongPin, correctPin}), // wrong + correct
		relay.WithTLSConfig(&tls.Config{InsecureSkipVerify: true}),   //nolint:gosec
	)

	resp, err = multiPinClient.Execute(multiPinClient.Get("/api"))
	if err != nil {
		log.Fatalf("unexpected error with multi-pin: %v", err)
	}
	fmt.Printf("  status: %d (accepted because correct pin was in list)\n\n", resp.StatusCode)

	// -------------------------------------------------------------------------
	// 5. WithCertificatePinning + WithBaseURL pattern for production.
	//
	// The pin is typically stored as a constant alongside the binary or pulled
	// from a secrets manager at startup. Never hard-code it in human-readable
	// source if it must stay private.
	// -------------------------------------------------------------------------
	fmt.Println("=== Production pattern ===")
	fmt.Println("  relay.New(")
	fmt.Println(`      relay.WithBaseURL("https://api.example.com"),`)
	fmt.Printf("      relay.WithCertificatePinning([]string{%q}),\n", correctPin)
	fmt.Println("  )")
	fmt.Println()
	fmt.Println("  // On pin mismatch Execute returns relay.ErrCertificatePinMismatch")
	fmt.Println("  // (wrapped in the TLS handshake error chain).")
	fmt.Println("  // Use errors.Is(err, relay.ErrCertificatePinMismatch) to detect it.")
}
