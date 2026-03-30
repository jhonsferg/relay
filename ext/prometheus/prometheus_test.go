package prometheus_test

import (
	"net/http"
	"testing"
	"time"

	promclient "github.com/prometheus/client_golang/prometheus"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/jhonsferg/relay"
	relayprom "github.com/jhonsferg/relay/ext/prometheus"
	"github.com/jhonsferg/relay/testutil"
)

func newRegistry() *promclient.Registry {
	return promclient.NewRegistry()
}

func TestWithPrometheus_CounterIncrements(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})

	reg := newRegistry()
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relayprom.WithPrometheus(reg, "test"),
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

	count, err := promtestutil.GatherAndCount(reg)
	if err != nil {
		t.Fatalf("GatherAndCount: %v", err)
	}
	if count == 0 {
		t.Error("expected at least one metric family registered")
	}
}

func TestWithPrometheus_NilRegistryUsesDefault(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	// nil registry = DefaultRegisterer; use a unique namespace to avoid
	// AlreadyRegisteredError conflicts with other tests.
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relayprom.WithPrometheus(nil, "testrelay_nil"),
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

func TestWithPrometheus_DuplicateRegistrationNoError(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	reg := newRegistry()

	// Creating two clients sharing the same registry and namespace should not
	// panic — AlreadyRegisteredError is silently ignored.
	c1 := relay.New(
		relay.WithBaseURL(srv.URL()),
		relayprom.WithPrometheus(reg, "dup"),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)
	c2 := relay.New(
		relay.WithBaseURL(srv.URL()),
		relayprom.WithPrometheus(reg, "dup"),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	for _, c := range []*relay.Client{c1, c2} {
		if _, err := c.Execute(c.Get("/")); err != nil {
			t.Fatalf("Execute: %v", err)
		}
	}
}

func TestWithPrometheus_ErrorRequestRecorded(t *testing.T) {
	t.Parallel()

	reg := newRegistry()
	c := relay.New(
		relay.WithTimeout(50*time.Millisecond),
		relayprom.WithPrometheus(reg, "errreg"),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	_, err := c.Execute(c.Get("http://127.0.0.1:1"))
	if err == nil {
		t.Fatal("expected transport error")
	}

	// The requests_total counter should be updated with status_code="error".
	count, gatherErr := promtestutil.GatherAndCount(reg)
	if gatherErr != nil {
		t.Fatalf("GatherAndCount: %v", gatherErr)
	}
	if count == 0 {
		t.Error("expected metrics even on transport error")
	}
}

func TestWithPrometheus_MultipleRequests(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	const n = 5
	for i := 0; i < n; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})
	}

	reg := newRegistry()
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relayprom.WithPrometheus(reg, "multi"),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	for i := 0; i < n; i++ {
		if _, err := c.Execute(c.Get("/")); err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
	}

	// Verify metrics are recorded.
	if _, err := promtestutil.GatherAndCount(reg); err != nil {
		t.Fatalf("GatherAndCount: %v", err)
	}
}
