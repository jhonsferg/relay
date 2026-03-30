package relay_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
)

func TestHealthCheck_ResetsOpenCircuit(t *testing.T) {
	// Health endpoint that always returns 200.
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthy.Close()

	// Trip the circuit breaker immediately by using MaxFailures=1 and a
	// very short ResetTimeout so the test does not wait for normal recovery.
	client := relay.New(
		relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
			MaxFailures:      1,
			ResetTimeout:     10 * time.Minute, // long — health check must beat this
			HalfOpenRequests: 1,
			SuccessThreshold: 1,
		}),
		relay.WithHealthCheck(healthy.URL, 50*time.Millisecond, 2*time.Second, 200),
		relay.WithDisableRetry(),
	)

	// Force the breaker open: hit a server that always 500s.
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv500.Close()

	client.Execute(client.Get(srv500.URL)) //nolint:errcheck // intentional 500

	if client.CircuitBreakerState() != relay.StateOpen {
		t.Fatal("expected circuit to be open after 500")
	}

	// Wait for health check to fire and reset the breaker.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if client.CircuitBreakerState() == relay.StateClosed {
			return // success
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("circuit breaker was not reset by health check within 2 s")
}

func TestHealthCheck_DoesNotPollWhenClosed(t *testing.T) {
	var probes atomic.Int32
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer health.Close()

	client := relay.New(
		relay.WithHealthCheck(health.URL, 10*time.Millisecond, time.Second, 200),
	)

	// Circuit is closed — health check goroutine should not probe.
	time.Sleep(60 * time.Millisecond)

	if n := probes.Load(); n > 0 {
		t.Errorf("health check probed %d times while circuit was closed, want 0", n)
	}
	client.Shutdown(t.Context()) //nolint:errcheck
}

func TestHealthCheck_UnhealthyEndpointDoesNotReset(t *testing.T) {
	// Health endpoint returns 503.
	sick := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer sick.Close()

	client := relay.New(
		relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
			MaxFailures:      1,
			ResetTimeout:     10 * time.Minute,
			HalfOpenRequests: 1,
			SuccessThreshold: 1,
		}),
		relay.WithHealthCheck(sick.URL, 20*time.Millisecond, time.Second, 200),
		relay.WithDisableRetry(),
	)

	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv500.Close()

	client.Execute(client.Get(srv500.URL)) //nolint:errcheck

	if client.CircuitBreakerState() != relay.StateOpen {
		t.Fatal("expected circuit to be open")
	}

	// Wait a bit — health check should NOT reset the breaker.
	time.Sleep(120 * time.Millisecond)

	if client.CircuitBreakerState() != relay.StateOpen {
		t.Error("circuit should remain open when health endpoint returns 503")
	}
	client.Shutdown(t.Context()) //nolint:errcheck
}

func TestHealthCheck_StopsOnShutdown(t *testing.T) {
	var probes atomic.Int32
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer health.Close()

	// Trip the circuit so health check starts polling.
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv500.Close()

	client := relay.New(
		relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
			MaxFailures:      1,
			ResetTimeout:     10 * time.Minute,
			HalfOpenRequests: 1,
			SuccessThreshold: 1,
		}),
		relay.WithHealthCheck(health.URL, 10*time.Millisecond, time.Second, 200),
		relay.WithDisableRetry(),
	)
	client.Execute(client.Get(srv500.URL)) //nolint:errcheck

	// Let a few probes happen, then shut down.
	time.Sleep(60 * time.Millisecond)
	client.Shutdown(t.Context()) //nolint:errcheck

	before := probes.Load()
	time.Sleep(50 * time.Millisecond)
	after := probes.Load()

	if after > before+1 {
		t.Errorf("health check continued polling after Shutdown (%d → %d probes)", before, after)
	}
}
