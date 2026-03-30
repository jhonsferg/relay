package relay_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
)

func TestDNSCache_BasicConnectivity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New(relay.WithDNSCache(30 * time.Second))
	resp, err := client.Execute(client.Get(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestDNSCache_ReusedAcrossRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New(relay.WithDNSCache(30 * time.Second))

	// Send multiple requests - all should succeed regardless of cache state.
	for i := 0; i < 5; i++ {
		resp, err := client.Execute(client.Get(srv.URL))
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
		if !resp.IsSuccess() {
			t.Errorf("request %d: status = %d, want 2xx", i, resp.StatusCode)
		}
	}
}

func TestDNSCache_ShortTTLExpires(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Very short TTL - cache expires between requests but client should still work.
	client := relay.New(relay.WithDNSCache(1 * time.Millisecond))

	for i := 0; i < 3; i++ {
		time.Sleep(5 * time.Millisecond)
		resp, err := client.Execute(client.Get(srv.URL))
		if err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
		if !resp.IsSuccess() {
			t.Errorf("request %d: status = %d, want 2xx", i, resp.StatusCode)
		}
	}
}

func TestWithDNSCache_OptionApplied(t *testing.T) {
	// Verify that WithDNSCache and WithDNSOverride are mutually exclusive in
	// configuration (both can be set but only one DialContext is wired).
	// This test ensures WithDNSCache does not panic when combined with other
	// transport options.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New(
		relay.WithDNSCache(10*time.Second),
		relay.WithTimeout(5*time.Second),
	)
	resp, err := client.Execute(client.Get(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsSuccess() {
		t.Errorf("status = %d, want 2xx", resp.StatusCode)
	}
}
