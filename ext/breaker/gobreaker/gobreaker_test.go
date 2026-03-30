package gobreaker_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	gb "github.com/sony/gobreaker"

	"github.com/jhonsferg/relay"
	relaybreaker "github.com/jhonsferg/relay/ext/breaker/gobreaker"
)

func settings(name string, maxFailures uint32, timeout time.Duration) gb.Settings {
	return gb.Settings{
		Name:        name,
		MaxRequests: 1,
		Interval:    0,
		Timeout:     timeout,
		ReadyToTrip: func(counts gb.Counts) bool {
			return counts.ConsecutiveFailures >= maxFailures
		},
	}
}

func TestWithGoBreaker_SuccessPassesThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cb := gb.NewCircuitBreaker(settings("test", 3, 30*time.Second))
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
		relaybreaker.WithGoBreaker(cb),
	)

	resp, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestWithGoBreaker_TripsOnConsecutiveFailures(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cb := relaybreaker.NewCircuitBreaker(settings("trip-test", 2, 60*time.Second))
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
		relaybreaker.WithGoBreaker(cb),
	)

	// Two 500 responses trip the breaker (ConsecutiveFailures >= 2).
	client.Execute(client.Get("/")) //nolint:errcheck - 500
	client.Execute(client.Get("/")) //nolint:errcheck - 500

	// Third call should be rejected by the open breaker (no server hit).
	_, err := client.Execute(client.Get("/"))
	if err == nil {
		t.Fatal("expected error from open breaker, got nil")
	}
	if !errors.Is(err, gb.ErrOpenState) {
		t.Errorf("error = %v, want ErrOpenState", err)
	}
	if hits.Load() != 2 {
		t.Errorf("server hits = %d, want 2 (third was blocked)", hits.Load())
	}
}

func TestWithGoBreaker_RecoveryAfterTimeout(t *testing.T) {
	var healthy atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if healthy.Load() {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	cb := relaybreaker.NewCircuitBreaker(settings("recovery", 2, 50*time.Millisecond))
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
		relaybreaker.WithGoBreaker(cb),
	)

	// Trip the breaker.
	client.Execute(client.Get("/")) //nolint:errcheck
	client.Execute(client.Get("/")) //nolint:errcheck

	// Wait for timeout → HalfOpen.
	healthy.Store(true)
	time.Sleep(70 * time.Millisecond)

	// Probe succeeds → breaker closes.
	resp, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("unexpected error after recovery: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestWithGoBreaker_NetworkErrorCountsAsFailure(t *testing.T) {
	// Use a closed port to simulate network errors.
	cb := relaybreaker.NewCircuitBreaker(settings("network-err", 2, 60*time.Second))
	client := relay.New(
		relay.WithBaseURL("http://127.0.0.1:1"), // no server at this port
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
		relaybreaker.WithGoBreaker(cb),
	)

	client.Execute(client.Get("/")) //nolint:errcheck - network error
	client.Execute(client.Get("/")) //nolint:errcheck - network error

	_, err := client.Execute(client.Get("/"))
	if !errors.Is(err, gb.ErrOpenState) {
		t.Errorf("error = %v, want ErrOpenState after network errors", err)
	}
}

func TestNewCircuitBreaker_DefaultIsSuccessful(t *testing.T) {
	// Verify NewCircuitBreaker sets a default IsSuccessful that doesn't panic.
	cb := relaybreaker.NewCircuitBreaker(gb.Settings{
		Name:    "defaults",
		Timeout: 30 * time.Second,
	})
	if cb == nil {
		t.Fatal("expected non-nil CircuitBreaker")
	}
}
