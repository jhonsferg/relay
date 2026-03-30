package jitterbug_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/ext/jitterbug"
)

// failThenSucceed returns an HTTP handler that fails the first n calls with
// statusCode, then succeeds with 200.
func failThenSucceed(n int, statusCode int) (http.Handler, *atomic.Int32) {
	var calls atomic.Int32
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := calls.Add(1)
		if int(c) <= n {
			w.WriteHeader(statusCode)
			fmt.Fprintf(w, `{"attempt":%d,"error":true}`, c)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"attempt":%d,"ok":true}`, c)
	}), &calls
}

func newClient(srv *httptest.Server, opt relay.Option) *relay.Client {
	return relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(5*time.Second),
		opt,
	)
}

// ---------------------------------------------------------------------------
// DecorrelatedJitter
// ---------------------------------------------------------------------------

func TestDecorrelatedJitter_RetriesAndSucceeds(t *testing.T) {
	t.Parallel()

	handler, calls := failThenSucceed(2, http.StatusServiceUnavailable)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c := newClient(srv, jitterbug.WithDecorrelatedJitter(jitterbug.Config{
		MaxAttempts: 5,
		Base:        1 * time.Millisecond,
		Cap:         10 * time.Millisecond,
	}))

	resp, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls = %d, want 3 (2 failures + 1 success)", got)
	}
}

func TestDecorrelatedJitter_ExhaustsMaxAttempts(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newClient(srv, jitterbug.WithDecorrelatedJitter(jitterbug.Config{
		MaxAttempts: 3,
		Base:        1 * time.Millisecond,
		Cap:         5 * time.Millisecond,
	}))

	resp, err := c.Execute(c.Get("/"))
	// On exhaustion the last response is returned (no error).
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls = %d, want 3", got)
	}
}

func TestDecorrelatedJitter_OnRetryCallback(t *testing.T) {
	t.Parallel()

	handler, _ := failThenSucceed(2, http.StatusTooManyRequests)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	var retryAttempts []int
	c := newClient(srv, jitterbug.WithDecorrelatedJitter(jitterbug.Config{
		MaxAttempts: 5,
		Base:        1 * time.Millisecond,
		Cap:         5 * time.Millisecond,
		OnRetry: func(attempt int, resp *http.Response, err error) {
			retryAttempts = append(retryAttempts, attempt)
		},
	}))

	if _, err := c.Execute(c.Get("/")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(retryAttempts) != 2 {
		t.Errorf("OnRetry called %d times, want 2", len(retryAttempts))
	}
	if retryAttempts[0] != 1 || retryAttempts[1] != 2 {
		t.Errorf("retry attempt numbers = %v, want [1 2]", retryAttempts)
	}
}

func TestDecorrelatedJitter_RetryIfPredicate(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// RetryIf returns false → should not retry despite 500.
	c := newClient(srv, jitterbug.WithDecorrelatedJitter(jitterbug.Config{
		MaxAttempts: 5,
		Base:        1 * time.Millisecond,
		RetryIf:     func(resp *http.Response, err error) bool { return false },
	}))

	if _, err := c.Execute(c.Get("/")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls = %d, want 1 (retry suppressed by RetryIf)", got)
	}
}

func TestDecorrelatedJitter_PostBodyReplay(t *testing.T) {
	t.Parallel()

	var bodiesReceived []string
	handler, _ := failThenSucceed(1, http.StatusServiceUnavailable)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf = make([]byte, 256)
		n, _ := r.Body.Read(buf)
		bodiesReceived = append(bodiesReceived, string(buf[:n]))
		handler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	c := newClient(srv, jitterbug.WithDecorrelatedJitter(jitterbug.Config{
		MaxAttempts: 3,
		Base:        1 * time.Millisecond,
		Cap:         5 * time.Millisecond,
	}))

	if _, err := c.Execute(c.Post("/").WithJSON(map[string]string{"key": "value"})); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for i, b := range bodiesReceived {
		if b == "" {
			t.Errorf("attempt %d received empty body — body not replayed", i+1)
		}
	}
}

// ---------------------------------------------------------------------------
// LinearBackoff
// ---------------------------------------------------------------------------

func TestLinearBackoff_RetriesAndSucceeds(t *testing.T) {
	t.Parallel()

	handler, calls := failThenSucceed(2, http.StatusBadGateway)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c := newClient(srv, jitterbug.WithLinearBackoff(jitterbug.Config{
		MaxAttempts: 5,
		Base:        1 * time.Millisecond,
		Cap:         20 * time.Millisecond,
	}, 0))

	resp, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls = %d, want 3", got)
	}
}

func TestLinearBackoff_WithJitter(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// jitterFactor=0.5 — just verify it doesn't panic and makes the calls.
	c := newClient(srv, jitterbug.WithLinearBackoff(jitterbug.Config{
		MaxAttempts: 3,
		Base:        1 * time.Millisecond,
		Cap:         10 * time.Millisecond,
	}, 0.5))

	c.Execute(c.Get("/")) //nolint:errcheck
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls = %d, want 3", got)
	}
}

func TestLinearBackoff_JitterFactorClamped(t *testing.T) {
	t.Parallel()

	// jitterFactor > 1 should be clamped to 1 — no panic.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newClient(srv, jitterbug.WithLinearBackoff(jitterbug.Config{MaxAttempts: 2}, 5.0))
	if _, err := c.Execute(c.Get("/")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

// ---------------------------------------------------------------------------
// RetryBudget
// ---------------------------------------------------------------------------

func TestRetryBudget_StopsWhenBudgetExhausted(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// Simulate a slow endpoint so budget exhausts quickly.
		time.Sleep(5 * time.Millisecond)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newClient(srv, jitterbug.WithRetryBudget(jitterbug.BudgetConfig{
		Config: jitterbug.Config{
			MaxAttempts: 20, // high — budget should kick in first
			Base:        1 * time.Millisecond,
			Cap:         5 * time.Millisecond,
		},
		TotalBudget: 30 * time.Millisecond, // exhausted after ~2-3 attempts
	}))

	c.Execute(c.Get("/")) //nolint:errcheck
	if n := calls.Load(); n >= 20 {
		t.Errorf("budget not respected: made %d calls (expected far fewer)", n)
	}
}

func TestRetryBudget_SucceedsWithinBudget(t *testing.T) {
	t.Parallel()

	handler, calls := failThenSucceed(1, http.StatusServiceUnavailable)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	c := newClient(srv, jitterbug.WithRetryBudget(jitterbug.BudgetConfig{
		Config: jitterbug.Config{
			MaxAttempts: 5,
			Base:        1 * time.Millisecond,
			Cap:         10 * time.Millisecond,
		},
		TotalBudget: 5 * time.Second, // generous budget
	}))

	resp, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("server calls = %d, want 2", got)
	}
}

func TestRetryBudget_DefaultBudgetApplied(t *testing.T) {
	t.Parallel()

	// Zero TotalBudget should default to 10 s — enough for this test.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := newClient(srv, jitterbug.WithRetryBudget(jitterbug.BudgetConfig{
		Config: jitterbug.Config{MaxAttempts: 3, Base: 1 * time.Millisecond},
		// TotalBudget: 0 → defaults to 10 s
	}))
	if _, err := c.Execute(c.Get("/")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Default Config
// ---------------------------------------------------------------------------

func TestConfig_ZeroValuesHaveDefaults(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// All zero config → defaults must be applied, no panic.
	c := newClient(srv, jitterbug.WithDecorrelatedJitter(jitterbug.Config{}))
	if _, err := c.Execute(c.Get("/")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}
