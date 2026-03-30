package metrics_test

import (
	"net/http"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric/noop"

	"github.com/jhonsferg/relay"
	relaymetrics "github.com/jhonsferg/relay/ext/metrics"
	"github.com/jhonsferg/relay/testutil"
)

func TestWithOTelMetrics_SuccessfulRequest(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})

	mp := noop.NewMeterProvider()
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relaymetrics.WithOTelMetrics(mp),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	resp, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestWithOTelMetrics_NilProviderFallsBackToGlobal(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	// nil falls back to global noop provider
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relaymetrics.WithOTelMetrics(nil),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	resp, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestWithOTelMetrics_ErrorRequest(t *testing.T) {
	t.Parallel()

	c := relay.New(
		relay.WithTimeout(50*time.Millisecond),
		relaymetrics.WithOTelMetrics(noop.NewMeterProvider()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	_, err := c.Execute(c.Get("http://127.0.0.1:1"))
	if err == nil {
		t.Fatal("expected transport error, got nil")
	}
}

func TestWithOTelMetrics_MultipleRequests(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	for i := 0; i < 3; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})
	}

	mp := noop.NewMeterProvider()
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relaymetrics.WithOTelMetrics(mp),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	for i := 0; i < 3; i++ {
		_, err := c.Execute(c.Get("/"))
		if err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
	}
}

func TestWithOTelMetrics_4xxResponse(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusBadRequest})

	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relaymetrics.WithOTelMetrics(noop.NewMeterProvider()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	resp, err := c.Execute(c.Get("/bad"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}
