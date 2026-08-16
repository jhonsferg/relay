package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRun_ExitCodeOnSuccess and TestRun_ExitCodeOnFailures guard against a
// regression where main() called os.Exit() directly at every exit point,
// skipping the deferred client.Shutdown() call. run() must reach its return
// statement on every path so deferred cleanup always executes.

func TestRun_ExitCodeOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	code := run([]string{"-n", "5", "-c", "2", "-q", srv.URL})
	if code != 0 {
		t.Errorf("run() = %d, want 0 for all-successful requests", code)
	}
}

func TestRun_ExitCodeOnFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	code := run([]string{"-n", "5", "-c", "2", "-q", srv.URL})
	if code != 1 {
		t.Errorf("run() = %d, want 1 when all requests return 5xx", code)
	}
}

func TestRun_NoArgsReturnsUsageError(t *testing.T) {
	if code := run(nil); code != 1 {
		t.Errorf("run(nil) = %d, want 1", code)
	}
}

func TestRun_VersionFlag(t *testing.T) {
	if code := run([]string{"-version"}); code != 0 {
		t.Errorf("run([-version]) = %d, want 0", code)
	}
}
