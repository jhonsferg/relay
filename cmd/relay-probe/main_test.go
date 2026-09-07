package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestBuildProbes(t *testing.T) {
	t.Parallel()

	urls := []string{"https://a.example.com", "https://b.example.com"}
	probes := buildProbes(urls, time.Second, 1, false, false)
	defer shutdownAll(probes)

	if len(probes) != 2 {
		t.Fatalf("len(probes) = %d, want 2", len(probes))
	}
	for i, p := range probes {
		if p.url != urls[i] {
			t.Errorf("probes[%d].url = %q, want %q", i, p.url, urls[i])
		}
		if p.client == nil {
			t.Errorf("probes[%d].client is nil", i)
		}
	}
}

func TestNewProbeClient_RetryEnabled(t *testing.T) {
	t.Parallel()

	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newProbeClient(srv.URL, time.Second, 2, false, true)
	defer func() { _ = c.Shutdown(context.Background()) }() //nolint:errcheck

	resp, err := c.Execute(c.Get(srv.URL))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if attempts < 2 {
		t.Errorf("attempts = %d, want at least 2 (retry should have kicked in)", attempts)
	}
}

func TestNewProbeClient_RetryDisabled(t *testing.T) {
	t.Parallel()

	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newProbeClient(srv.URL, time.Second, 0, false, false)
	defer func() { _ = c.Shutdown(context.Background()) }() //nolint:errcheck

	if _, err := c.Execute(c.Get(srv.URL)); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want exactly 1 with retries disabled", attempts)
	}
}

func TestNewProbeClient_CircuitBreakerEnabled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newProbeClient(srv.URL, time.Second, 0, true, false)
	defer func() { _ = c.Shutdown(context.Background()) }() //nolint:errcheck

	if !c.IsHealthy() {
		t.Error("client should start healthy before any failures")
	}
}

func TestShutdownAll(t *testing.T) {
	t.Parallel()

	probes := buildProbes([]string{"https://a.example.com"}, time.Second, 0, false, false)
	// Should not panic or block.
	shutdownAll(probes)
}
