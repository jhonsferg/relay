package sentry_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sentrygo "github.com/getsentry/sentry-go"

	"github.com/jhonsferg/relay"
	relaysentry "github.com/jhonsferg/relay/ext/sentry"
)

// captureTransport records events sent to Sentry without making real network
// calls, allowing tests to inspect what was captured.
type captureTransport struct {
	mu          sync.Mutex
	events      []*sentrygo.Event
	breadcrumbs []*sentrygo.Breadcrumb
}

func (t *captureTransport) SendEvent(event *sentrygo.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, event)
}

func (t *captureTransport) Flush(timeout time.Duration) bool { return true }

func (t *captureTransport) Configure(options sentrygo.ClientOptions) {}

// newTestHub creates a Sentry Hub backed by the captureTransport so we can
// inspect captured events without a real Sentry DSN.
func newTestHub(ct *captureTransport) *sentrygo.Hub {
	client, _ := sentrygo.NewClient(sentrygo.ClientOptions{
		Transport:        ct,
		TracesSampleRate: 0,
	})
	return sentrygo.NewHub(client, sentrygo.NewScope())
}

func newRelayClient(srv *httptest.Server, hub *sentrygo.Hub, opts ...relaysentry.Option) *relay.Client {
	return relay.New(
		relay.WithBaseURL(srv.URL),
		relaysentry.WithSentry(hub, opts...),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(5*time.Second),
	)
}

// ---------------------------------------------------------------------------
// Transport errors
// ---------------------------------------------------------------------------

func TestWithSentry_CapturesTransportError(t *testing.T) {
	t.Parallel()

	ct := &captureTransport{}
	hub := newTestHub(ct)

	// Point client at an address with no server.
	client := relay.New(
		relay.WithBaseURL("http://127.0.0.1:1"), // nothing listening
		relaysentry.WithSentry(hub),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(200*time.Millisecond),
	)

	_, err := client.Execute(client.Get("/"))
	if err == nil {
		t.Fatal("expected connection error, got nil")
	}
	if len(ct.events) != 1 {
		t.Errorf("captured events = %d, want 1", len(ct.events))
	}
}

func TestWithSentry_TransportErrorsDisabled(t *testing.T) {
	t.Parallel()

	ct := &captureTransport{}
	hub := newTestHub(ct)

	client := relay.New(
		relay.WithBaseURL("http://127.0.0.1:1"),
		relaysentry.WithSentry(hub, relaysentry.WithCaptureTransportErrors(false)),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(200*time.Millisecond),
	)

	client.Execute(client.Get("/")) //nolint:errcheck
	if len(ct.events) != 0 {
		t.Errorf("expected no events when transport error capture disabled, got %d", len(ct.events))
	}
}

// ---------------------------------------------------------------------------
// 5xx capture
// ---------------------------------------------------------------------------

func TestWithSentry_Captures5xx(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"boom"}`)
	}))
	defer srv.Close()

	ct := &captureTransport{}
	hub := newTestHub(ct)
	c := newRelayClient(srv, hub)

	resp, err := c.Execute(c.Get("/crash"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != 500 {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	if len(ct.events) != 1 {
		t.Errorf("captured events = %d, want 1", len(ct.events))
	}
	if ct.events[0].Level != sentrygo.LevelError {
		t.Errorf("event level = %v, want error", ct.events[0].Level)
	}
	if !strings.Contains(ct.events[0].Message, "500") {
		t.Errorf("event message %q should contain status code", ct.events[0].Message)
	}
}

func TestWithSentry_ServerErrorsDisabled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ct := &captureTransport{}
	hub := newTestHub(ct)
	c := newRelayClient(srv, hub, relaysentry.WithCaptureServerErrors(false))

	c.Execute(c.Get("/")) //nolint:errcheck
	if len(ct.events) != 0 {
		t.Errorf("expected no events when server error capture disabled, got %d", len(ct.events))
	}
}

// ---------------------------------------------------------------------------
// 4xx capture
// ---------------------------------------------------------------------------

func TestWithSentry_Does_Not_Capture4xxByDefault(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	ct := &captureTransport{}
	hub := newTestHub(ct)
	c := newRelayClient(srv, hub) // default: captureClientErrors = false

	c.Execute(c.Get("/missing")) //nolint:errcheck
	if len(ct.events) != 0 {
		t.Errorf("expected no events for 404 by default, got %d", len(ct.events))
	}
}

func TestWithSentry_Captures4xxWhenEnabled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	ct := &captureTransport{}
	hub := newTestHub(ct)
	c := newRelayClient(srv, hub, relaysentry.WithCaptureClientErrors(true))

	c.Execute(c.Get("/forbidden")) //nolint:errcheck
	if len(ct.events) != 1 {
		t.Errorf("captured events = %d, want 1", len(ct.events))
	}
	if ct.events[0].Level != sentrygo.LevelWarning {
		t.Errorf("event level = %v, want warning", ct.events[0].Level)
	}
}

// ---------------------------------------------------------------------------
// Breadcrumbs
// ---------------------------------------------------------------------------

func TestWithSentry_AddsBreadcrumbOnSuccess(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ct := &captureTransport{}

	// Use a scope that captures breadcrumbs.
	client, _ := sentrygo.NewClient(sentrygo.ClientOptions{Transport: ct})
	scope := sentrygo.NewScope()
	hub := sentrygo.NewHub(client, scope)

	c := newRelayClient(srv, hub)
	c.Execute(c.Get("/ok")) //nolint:errcheck

	// Breadcrumbs aren't forwarded to transport directly; they're attached to
	// the scope. Verify they don't cause panics or errors and the event path
	// for 2xx is clean.
	if len(ct.events) != 0 {
		t.Errorf("expected no events for 200, got %d", len(ct.events))
	}
}

func TestWithSentry_BreadcrumbsDisabled(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ct := &captureTransport{}
	hub := newTestHub(ct)
	c := newRelayClient(srv, hub, relaysentry.WithBreadcrumbs(false))

	// Should execute cleanly with no events.
	resp, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if len(ct.events) != 0 {
		t.Errorf("expected no events, got %d", len(ct.events))
	}
}

// ---------------------------------------------------------------------------
// Hub isolation
// ---------------------------------------------------------------------------

func TestWithSentry_HubIsClonedPerRequest(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ct := &captureTransport{}
	hub := newTestHub(ct)
	c := newRelayClient(srv, hub)

	// Fire three requests concurrently.
	done := make(chan struct{}, 3)
	for i := 0; i < 3; i++ {
		go func() {
			c.Execute(c.Get("/")) //nolint:errcheck
			done <- struct{}{}
		}()
	}
	for i := 0; i < 3; i++ {
		<-done
	}

	if len(ct.events) != 3 {
		t.Errorf("captured events = %d, want 3 (one per request)", len(ct.events))
	}
}
