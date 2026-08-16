package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRun_ExitCodeOnHealthy and TestRun_ExitCodeOnUnhealthy guard against a
// regression where main() called os.Exit() directly at every exit point,
// skipping the deferred shutdownAll(probes) call. run() must reach its
// return statement on every path so deferred cleanup always executes.

func TestRun_ExitCodeOnHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	code := run([]string{"-retry", "0", srv.URL})
	if code != 0 {
		t.Errorf("run() = %d, want 0 for a healthy endpoint", code)
	}
}

func TestRun_ExitCodeOnUnhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	code := run([]string{"-retry", "0", srv.URL})
	if code != 1 {
		t.Errorf("run() = %d, want 1 for an unhealthy endpoint", code)
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
