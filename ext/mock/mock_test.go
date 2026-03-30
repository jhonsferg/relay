package mock_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
	relaymock "github.com/jhonsferg/relay/ext/mock"
)

func client(mt *relaymock.MockTransport) *relay.Client {
	return relay.New(
		relay.WithBaseURL("http://api.example.com"),
		relay.WithDisableRetry(),
		relaymock.WithMock(mt),
	)
}

// -- Rule matching -------------------------------------------------------------

func TestMatchMethod(t *testing.T) {
	mt := relaymock.New()
	mt.On(relaymock.MatchMethod("GET")).RespondJSON(200, map[string]any{"ok": true})

	c := client(mt)
	resp, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestMatchPath(t *testing.T) {
	mt := relaymock.New()
	mt.On(relaymock.MatchPath("/users/42")).RespondJSON(200, map[string]any{"id": 42})

	c := client(mt)
	resp, err := c.Execute(c.Get("/users/42"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestMatchPathPrefix(t *testing.T) {
	mt := relaymock.New()
	mt.On(relaymock.MatchPathPrefix("/api/")).RespondJSON(200, nil)

	c := client(mt)
	for _, path := range []string{"/api/users", "/api/orders", "/api/products"} {
		resp, err := c.Execute(c.Get(path))
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", path, err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("%s: status = %d, want 200", path, resp.StatusCode)
		}
	}
}

func TestMatchHeader(t *testing.T) {
	mt := relaymock.New()
	mt.On(relaymock.MatchHeader("X-API-Key", "secret")).RespondJSON(200, nil)
	mt.On(relaymock.MatchAny()).Respond(401, nil)

	c := client(mt)

	// Without key → 401
	resp, err := c.Execute(c.Get("/secure"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 401 {
		t.Errorf("without key: status = %d, want 401", resp.StatusCode)
	}

	// With key → 200
	resp, err = c.Execute(c.Get("/secure").WithAPIKey("X-API-Key", "secret"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("with key: status = %d, want 200", resp.StatusCode)
	}
}

func TestMatchQueryParam(t *testing.T) {
	mt := relaymock.New()
	mt.On(relaymock.MatchQueryParam("locale", "en")).RespondJSON(200, map[string]any{"lang": "en"})
	mt.On(relaymock.MatchAny()).RespondJSON(200, map[string]any{"lang": "default"})

	c := client(mt)
	resp, err := c.Execute(c.Get("/i18n").WithQueryParam("locale", "en"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 || !contains(resp.String(), "en") {
		t.Errorf("body = %q, want 'en'", resp.String())
	}
}

// -- Responses -----------------------------------------------------------------

func TestRespondJSON(t *testing.T) {
	mt := relaymock.New()
	mt.On(relaymock.MatchAny()).RespondJSON(201, map[string]any{"id": 99})

	c := client(mt)
	resp, err := c.Execute(c.Post("/items").WithBody([]byte(`{}`)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 201 {
		t.Errorf("status = %d, want 201", resp.StatusCode)
	}
	if ct := resp.ContentType(); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestRespondError(t *testing.T) {
	mt := relaymock.New()
	sentinelErr := errors.New("connection refused")
	mt.On(relaymock.MatchAny()).RespondError(sentinelErr)

	c := client(mt)
	_, err := c.Execute(c.Get("/"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinelErr) {
		t.Errorf("error = %v, want %v", err, sentinelErr)
	}
}

func TestNoMatchingRule(t *testing.T) {
	mt := relaymock.New()
	// No rules registered.

	c := client(mt)
	_, err := c.Execute(c.Get("/nowhere"))
	if err == nil {
		t.Fatal("expected ErrNoMatchingRule, got nil")
	}
	if !errors.Is(err, relaymock.ErrNoMatchingRule) {
		t.Errorf("error = %v, want ErrNoMatchingRule", err)
	}
}

// -- Sequence ------------------------------------------------------------------

func TestRespondSequence_RetrySimulation(t *testing.T) {
	mt := relaymock.New()
	mt.On(relaymock.MatchAny()).RespondSequence(
		relaymock.Seq(500, []byte(`{"attempt":1}`), nil),
		relaymock.Seq(500, []byte(`{"attempt":2}`), nil),
		relaymock.Seq(200, []byte(`{"attempt":3}`), nil),
	)

	c := relay.New(
		relay.WithBaseURL("http://api.example.com"),
		relay.WithRetry(&relay.RetryConfig{
			MaxAttempts:     3,
			RetryableStatus: []int{500},
		}),
		relaymock.WithMock(mt),
	)

	resp, err := c.Execute(c.Get("/flaky"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if mt.CallCount() != 3 {
		t.Errorf("call count = %d, want 3", mt.CallCount())
	}
}

func TestRespondSequence_RepeatsLast(t *testing.T) {
	mt := relaymock.New()
	mt.On(relaymock.MatchAny()).RespondSequence(
		relaymock.Seq(200, []byte(`"first"`), nil),
		relaymock.Seq(200, []byte(`"last"`), nil),
	)

	c := client(mt)
	// Third call should repeat the second (last) entry.
	var bodies []string
	for i := 0; i < 4; i++ {
		resp, _ := c.Execute(c.Get("/"))
		if resp != nil {
			bodies = append(bodies, resp.String())
		}
	}
	if len(bodies) != 4 {
		t.Fatalf("got %d bodies, want 4", len(bodies))
	}
	if bodies[2] != `"last"` || bodies[3] != `"last"` {
		t.Errorf("bodies[2:] = %v, want [last last]", bodies[2:])
	}
}

func TestRespondSequence_WithErrors(t *testing.T) {
	mt := relaymock.New()
	networkErr := errors.New("connection reset")
	mt.On(relaymock.MatchAny()).RespondSequence(
		relaymock.SeqError(networkErr),
		relaymock.Seq(200, nil, nil),
	)

	c := relay.New(
		relay.WithBaseURL("http://api.example.com"),
		relay.WithRetry(&relay.RetryConfig{MaxAttempts: 2}),
		relaymock.WithMock(mt),
	)

	resp, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("unexpected error after retry: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if mt.CallCount() != 2 {
		t.Errorf("call count = %d, want 2", mt.CallCount())
	}
}

// -- Assertions ----------------------------------------------------------------

func TestAssertCallCount(t *testing.T) {
	mt := relaymock.New()
	mt.On(relaymock.MatchAny()).RespondJSON(200, nil)
	c := client(mt)

	for i := 0; i < 3; i++ {
		c.Execute(c.Get("/")) //nolint:errcheck
	}

	mt.AssertCallCount(t, 3)
}

func TestAssertCalled(t *testing.T) {
	mt := relaymock.New()
	mt.On(relaymock.MatchAny()).RespondJSON(200, nil)
	c := client(mt)

	c.Execute(c.Get("/users/1").WithBearerToken("tok")) //nolint:errcheck

	mt.AssertCalled(t,
		relaymock.MatchMethod("GET"),
		relaymock.MatchPath("/users/1"),
		relaymock.MatchHeader("Authorization", "Bearer tok"),
	)
}

func TestAssertNotCalled(t *testing.T) {
	mt := relaymock.New()
	mt.On(relaymock.MatchAny()).RespondJSON(200, nil)
	c := client(mt)

	c.Execute(c.Get("/public")) //nolint:errcheck

	mt.AssertNotCalled(t, relaymock.MatchPath("/admin"))
}

func TestReset(t *testing.T) {
	mt := relaymock.New()
	mt.On(relaymock.MatchAny()).RespondJSON(200, nil)
	c := client(mt)

	c.Execute(c.Get("/")) //nolint:errcheck
	if mt.CallCount() != 1 {
		t.Fatalf("want 1 call before reset, got %d", mt.CallCount())
	}

	mt.Reset()
	if mt.CallCount() != 0 {
		t.Errorf("call count after Reset = %d, want 0", mt.CallCount())
	}
	// Rules are also reset — now ErrNoMatchingRule.
	_, err := c.Execute(c.Get("/"))
	if !errors.Is(err, relaymock.ErrNoMatchingRule) {
		t.Errorf("after Reset: error = %v, want ErrNoMatchingRule", err)
	}
}

// -- Fallback ------------------------------------------------------------------

func TestFallback(t *testing.T) {
	var fallbackCalled bool
	fallback := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		fallbackCalled = true
		return &http.Response{
			StatusCode: 202,
			Status:     "202 Accepted",
			Header:     make(http.Header),
			Body:       http.NoBody,
			Proto:      "HTTP/1.1",
		}, nil
	})

	mt := relaymock.New().WithFallback(fallback)
	// No rules — all requests go to fallback.
	c := client(mt)

	resp, err := c.Execute(c.Get("/unmatched"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fallbackCalled {
		t.Error("fallback was not called")
	}
	if resp.StatusCode != 202 {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}
}

// -- Concurrency ---------------------------------------------------------------

func TestConcurrentRequests(t *testing.T) {
	mt := relaymock.New()
	mt.On(relaymock.MatchAny()).RespondJSON(200, nil)
	c := client(mt)

	done := make(chan struct{}, 20)
	for i := 0; i < 20; i++ {
		go func() {
			c.Execute(c.Get("/")) //nolint:errcheck
			done <- struct{}{}
		}()
	}
	timeout := time.After(5 * time.Second)
	for i := 0; i < 20; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("timeout waiting for concurrent requests")
		}
	}
	mt.AssertCallCount(t, 20)
}

// -- Helpers -------------------------------------------------------------------

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}
func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
