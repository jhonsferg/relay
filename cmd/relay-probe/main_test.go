package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
