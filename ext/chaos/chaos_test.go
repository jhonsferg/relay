package chaos_test

import (
	"errors"
	"io"
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

// trackingBody wraps an io.Reader and records whether Close was called.
type trackingBody struct {
	io.Reader
	closed bool
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// TestErrorRate_ClosesRequestBody guards against a leak where injecting
// ErrChaosInjected short-circuits before the base transport runs - the only
// place that would otherwise close req.Body - violating http.RoundTripper's
// contract that the body must always be closed, including on error paths.
func TestErrorRate_ClosesRequestBody(t *testing.T) {
	srv := newTestServer(t)
	var body *trackingBody
	tagBody := relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body = &trackingBody{Reader: req.Body}
			req.Body = body
			return next.RoundTrip(req)
		})
	})
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		tagBody,
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
		chaos.Middleware(chaos.Config{ErrorRate: 1.0}),
	)

	_, err := client.Execute(client.Post("/").WithBody([]byte("payload")))
	if !errors.Is(err, chaos.ErrChaosInjected) {
		t.Fatalf("expected ErrChaosInjected, got %v", err)
	}
	if body == nil {
		t.Fatal("tagBody middleware never ran")
	}
	if !body.closed {
		t.Error("req.Body was not closed when ErrorRate injection short-circuited the request")
	}
}

// TestFaultRate_ClosesRequestBody is the same guard as
// TestErrorRate_ClosesRequestBody, for the fabricated-response fault path.
func TestFaultRate_ClosesRequestBody(t *testing.T) {
	srv := newTestServer(t)
	var body *trackingBody
	tagBody := relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body = &trackingBody{Reader: req.Body}
			req.Body = body
			return next.RoundTrip(req)
		})
	})
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		tagBody,
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRetry(),
		chaos.Middleware(chaos.Config{Faults: []int{503}, FaultRate: 1.0}),
	)

	resp, err := client.Execute(client.Post("/").WithBody([]byte("payload")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 503 {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	if body == nil {
		t.Fatal("tagBody middleware never ran")
	}
	if !body.closed {
		t.Error("req.Body was not closed when FaultRate injection short-circuited the request")
	}
}
