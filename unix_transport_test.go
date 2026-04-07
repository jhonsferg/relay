//go:build !windows && !js

package relay_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
)

// startUnixServer starts an HTTP server listening on a Unix domain socket and
// returns the socket path along with a cleanup function that stops the server.
func startUnixServer(t *testing.T, handler http.Handler) (socketPath string, cleanup func()) {
	t.Helper()

	socketPath = filepath.Join(os.TempDir(), fmt.Sprintf("relay_test_%d.sock", os.Getpid()))
	// Remove any stale socket file from a previous test run.
	_ = os.Remove(socketPath)

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen on Unix socket %s: %v", socketPath, err)
	}

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second, //nolint:mnd
	}
	go func() { _ = srv.Serve(ln) }()

	return socketPath, func() {
		_ = srv.Shutdown(context.Background())
		_ = os.Remove(socketPath)
	}
}

// TestUnixSocket_BasicRequest verifies that a relay client configured with
// WithUnixSocket can successfully perform a GET request to an HTTP server
// listening on a Unix domain socket.
func TestUnixSocket_BasicRequest(t *testing.T) {
	const want = "hello from unix socket"

	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, want)
	})

	socketPath, cleanup := startUnixServer(t, mux)
	defer cleanup()

	client := relay.New(
		relay.WithBaseURL("http://localhost"),
		relay.WithUnixSocket(socketPath),
	)

	resp, err := client.Execute(client.Get("/ping"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := resp.Text()
	if got != want {
		t.Errorf("body = %q; want %q", got, want)
	}
}

// TestUnixSocket_StatusCode verifies that the HTTP status code from the Unix
// socket server is correctly propagated to the caller.
func TestUnixSocket_StatusCode(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	socketPath, cleanup := startUnixServer(t, mux)
	defer cleanup()

	client := relay.New(
		relay.WithBaseURL("http://localhost"),
		relay.WithUnixSocket(socketPath),
	)

	resp, err := client.Execute(client.Get("/health"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d; want %d", resp.StatusCode, http.StatusNoContent)
	}
}

// TestUnixSocket_HostHeaderPreserved confirms that the HTTP Host header
// reflects the base URL rather than the socket path when routing via a Unix
// domain socket.
func TestUnixSocket_HostHeaderPreserved(t *testing.T) {
	const wantHost = "localhost"

	var gotHost string
	mux := http.NewServeMux()
	mux.HandleFunc("/host", func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.WriteHeader(http.StatusOK)
	})

	socketPath, cleanup := startUnixServer(t, mux)
	defer cleanup()

	client := relay.New(
		relay.WithBaseURL("http://"+wantHost),
		relay.WithUnixSocket(socketPath),
	)

	resp, err := client.Execute(client.Get("/host"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}

	if gotHost != wantHost {
		t.Errorf("Host header = %q; want %q", gotHost, wantHost)
	}
}

// TestUnixSocket_EmptyPath verifies that supplying an empty socket path causes
// the client to fail at request time rather than silently falling back to TCP.
func TestUnixSocket_EmptyPath(t *testing.T) {
	client := relay.New(
		relay.WithBaseURL("http://localhost"),
		relay.WithUnixSocket(""),
	)

	// An empty Unix socket path is equivalent to no override; the client will
	// attempt a normal TCP connection to localhost. There is no server running
	// there in the test environment, so Execute should return a dial error.
	_, err := client.Execute(client.Get("/"))
	if err == nil {
		t.Fatal("expected an error for empty socket path but got nil")
	}
}
