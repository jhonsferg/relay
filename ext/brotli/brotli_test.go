package brotli_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	brotlienc "github.com/andybalholm/brotli"

	"github.com/jhonsferg/relay"
	relaybrotli "github.com/jhonsferg/relay/ext/brotli"
)

// brotliBody returns brotli-compressed bytes for the given string.
func brotliBody(s string) []byte {
	var buf bytes.Buffer
	w := brotlienc.NewWriter(&buf)
	w.Write([]byte(s)) //nolint:errcheck
	w.Close()          //nolint:errcheck
	return buf.Bytes()
}

func TestWithBrotliDecompression_DecompressesResponse(t *testing.T) {
	t.Parallel()

	const want = "hello brotli world"
	compressed := brotliBody(want)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "br")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write(compressed) //nolint:errcheck
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relaybrotli.WithBrotliDecompression(),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	resp, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if got := resp.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestWithBrotliDecompression_AddsAcceptEncoding(t *testing.T) {
	t.Parallel()

	var capturedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("Accept-Encoding")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relaybrotli.WithBrotliDecompression(),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	_, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(capturedHeader, "br") {
		t.Errorf("expected Accept-Encoding to contain 'br', got %q", capturedHeader)
	}
}

func TestWithBrotliDecompression_PreservesExistingAcceptEncoding(t *testing.T) {
	t.Parallel()

	var capturedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("Accept-Encoding")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relaybrotli.WithBrotliDecompression(),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	req := c.Get("/")
	req = req.WithHeader("Accept-Encoding", "gzip")
	_, err := c.Execute(req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(capturedHeader, "br") {
		t.Errorf("expected Accept-Encoding to contain 'br', got %q", capturedHeader)
	}
	if !strings.Contains(capturedHeader, "gzip") {
		t.Errorf("expected Accept-Encoding to contain 'gzip', got %q", capturedHeader)
	}
}

func TestWithBrotliDecompression_NonBrotliResponsePassthrough(t *testing.T) {
	t.Parallel()

	const want = "plain text response"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No Content-Encoding header — plain text.
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(want)) //nolint:errcheck
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relaybrotli.WithBrotliDecompression(),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	resp, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := resp.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestWithBrotliDecompression_CaseInsensitiveContentEncoding(t *testing.T) {
	t.Parallel()

	const want = "case insensitive"
	compressed := brotliBody(want)

	// Use "BR" (uppercase) to test case-insensitive matching.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "BR")
		w.WriteHeader(http.StatusOK)
		w.Write(compressed) //nolint:errcheck
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relaybrotli.WithBrotliDecompression(),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	resp, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := resp.String(); got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}
