package chaos_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
	chaos "github.com/jhonsferg/relay/ext/chaos"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newClient builds a relay client pointing at srv with the given chaos option.
// A very large MaxFailures is used so the circuit breaker never trips during tests.
func newClient(t *testing.T, srv *httptest.Server, opt relay.Option) *relay.Client {
	t.Helper()
	return relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
			MaxFailures:      1_000_000,
			ResetTimeout:     time.Hour,
			HalfOpenRequests: 1,
			SuccessThreshold: 1,
		}),
		relay.WithDisableRetry(),
		opt,
	)
}

func TestErrorRate_Always(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t, srv, chaos.Middleware(chaos.Config{ErrorRate: 1.0}))

	_, err := client.Execute(client.Get("/"))
	if !errors.Is(err, chaos.ErrChaosInjected) {
		t.Fatalf("expected ErrChaosInjected, got %v", err)
	}
}

func TestErrorRate_Never(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t, srv, chaos.Middleware(chaos.Config{ErrorRate: 0.0}))

	resp, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestLatencyRate_Always(t *testing.T) {
	srv := newTestServer(t)
	const injectLatency = 50 * time.Millisecond
	client := newClient(t, srv, chaos.Middleware(chaos.Config{
		LatencyRate: 1.0,
		Latency:     injectLatency,
	}))

	start := time.Now()
	resp, err := client.Execute(client.Get("/"))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if elapsed < injectLatency {
		t.Fatalf("expected at least %v latency, got %v", injectLatency, elapsed)
	}
}

func TestFaultStatus_Always503(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t, srv, chaos.Middleware(chaos.Config{
		Faults:    []int{http.StatusServiceUnavailable},
		FaultRate: 1.0,
	}))

	resp, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", resp.StatusCode)
	}
}

func TestFaultRate_Never(t *testing.T) {
	srv := newTestServer(t)
	client := newClient(t, srv, chaos.Middleware(chaos.Config{
		Faults:    []int{http.StatusServiceUnavailable},
		FaultRate: 0.0,
	}))

	resp, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

func TestMultipleFaults_RandomSelection(t *testing.T) {
	srv := newTestServer(t)
	faults := []int{
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	}
	client := newClient(t, srv, chaos.Middleware(chaos.Config{
		Faults:    faults,
		FaultRate: 1.0,
	}))

	seen := make(map[int]bool)
	// Run enough iterations to have high probability of seeing all three codes.
	for range 300 {
		resp, err := client.Execute(client.Get("/"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		seen[resp.StatusCode] = true
	}

	for _, code := range faults {
		if !seen[code] {
			t.Errorf("fault code %d was never injected in 300 iterations", code)
		}
	}
}
