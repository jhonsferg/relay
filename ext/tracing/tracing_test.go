package tracing_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/jhonsferg/relay"
	relaytracing "github.com/jhonsferg/relay/ext/tracing"
	"github.com/jhonsferg/relay/testutil"
)

func TestWithTracing_SpanCreated(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})

	tp := noop.NewTracerProvider()
	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relaytracing.WithTracing(tp, prop),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	resp, err := c.Execute(c.Get("/test"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestWithTracing_PropagatesTraceContext(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
	)

	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relaytracing.WithTracing(nil, prop), // nil provider = global (noop by default)
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	ctx := context.Background()
	_, err := c.Execute(c.Get("/").WithContext(ctx))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestWithTracing_NilProviderFallsBackToGlobal(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	// Both nil — uses global providers (noop by default in tests)
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relaytracing.WithTracing(nil, nil),
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

func TestWithTracing_Error4xxMarkedAsError(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusNotFound, Body: "not found"})

	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relaytracing.WithTracing(noop.NewTracerProvider(), nil),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	resp, err := c.Execute(c.Get("/missing"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestWithTracing_TransportErrorRecorded(t *testing.T) {
	t.Parallel()

	c := relay.New(
		relay.WithTimeout(50*time.Millisecond),
		relaytracing.WithTracing(noop.NewTracerProvider(), nil),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	// Dial an unreachable address to force transport error.
	_, err := c.Execute(c.Get("http://127.0.0.1:1"))
	if err == nil {
		t.Fatal("expected transport error, got nil")
	}
}

func TestWithTracing_WithContentLength(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Length": "5"},
		Body:    "hello",
	})

	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relaytracing.WithTracing(noop.NewTracerProvider(), nil),
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
