package relay

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

func TestRetry_CorrectNumberOfAttempts(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	// Enqueue 2 failures then 1 success; the client is configured for 3 attempts.
	srv.Enqueue(testutil.MockResponse{Status: http.StatusServiceUnavailable})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusServiceUnavailable})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "recovered"})

	c := New(
		WithDisableCircuitBreaker(),
		WithRetry(&RetryConfig{
			MaxAttempts:     3,
			InitialInterval: 1 * time.Millisecond,
			MaxInterval:     5 * time.Millisecond,
			Multiplier:      1.0,
			RandomFactor:    0,
			RetryableStatus: []int{http.StatusServiceUnavailable},
		}),
	)

	resp, err := c.Execute(c.Get(srv.URL() + "/flaky"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if srv.RequestCount() != 3 {
		t.Errorf("expected 3 total attempts, got %d", srv.RequestCount())
	}
}

func TestRetry_ExhaustsAllAttempts(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	// Enqueue enough 500s to exhaust all 3 attempts.
	for i := 0; i < 5; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	}

	c := New(
		WithDisableCircuitBreaker(),
		WithRetry(&RetryConfig{
			MaxAttempts:     3,
			InitialInterval: 1 * time.Millisecond,
			MaxInterval:     5 * time.Millisecond,
			Multiplier:      1.0,
			RandomFactor:    0,
			RetryableStatus: []int{http.StatusInternalServerError},
		}),
	)

	resp, err := c.Execute(c.Get(srv.URL() + "/broken"))
	// All retries exhausted: Execute returns the last response without error.
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500 on final attempt, got %d", resp.StatusCode)
	}
	if srv.RequestCount() != 3 {
		t.Errorf("expected exactly 3 attempts, got %d", srv.RequestCount())
	}
}

func TestRetry_RetryAfterHeaderHonored(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	// First response: 429 with Retry-After: 1 (1 second is too long; use a tiny
	// custom value by returning "0" so the wait is effectively 0).
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusTooManyRequests,
		Headers: map[string]string{"Retry-After": "0"},
	})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})

	c := New(
		WithDisableCircuitBreaker(),
		WithRetry(&RetryConfig{
			MaxAttempts:     2,
			InitialInterval: 1 * time.Millisecond,
			MaxInterval:     5 * time.Millisecond,
			Multiplier:      1.0,
			RandomFactor:    0,
			RetryableStatus: []int{http.StatusTooManyRequests},
		}),
	)

	start := time.Now()
	resp, err := c.Execute(c.Get(srv.URL() + "/rate"))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	// Retry-After: 0 should produce negligible wait.
	if elapsed > 500*time.Millisecond {
		t.Errorf("retry took too long for Retry-After: 0, elapsed %v", elapsed)
	}
}

func TestRetry_RetryAfterDelayRespected(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	// Enqueue a 429 with Retry-After: 2 (2 seconds).
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusTooManyRequests,
		Headers: map[string]string{"Retry-After": "2"},
	})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(
		WithDisableCircuitBreaker(),
		WithRetry(&RetryConfig{
			MaxAttempts:     2,
			InitialInterval: 1 * time.Millisecond,
			MaxInterval:     5 * time.Second,
			Multiplier:      1.0,
			RandomFactor:    0,
			RetryableStatus: []int{http.StatusTooManyRequests},
		}),
	)

	start := time.Now()
	resp, err := c.Execute(c.Get(srv.URL() + "/rate"))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	// Retry-After: 2 should add at least ~2 seconds of wait.
	// We use a safe margin of 1.5s for CI.
	if elapsed < 1500*time.Millisecond {
		t.Errorf("expected at least 1.5s delay from Retry-After header, elapsed %v", elapsed)
	}
}

func TestRetry_CustomRetryIfPredicateSuppresses(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusServiceUnavailable})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK}) // should NOT be reached

	// RetryIf suppresses the retry.
	c := New(
		WithDisableCircuitBreaker(),
		WithRetry(&RetryConfig{
			MaxAttempts:     3,
			InitialInterval: 1 * time.Millisecond,
			MaxInterval:     5 * time.Millisecond,
			Multiplier:      1.0,
			RandomFactor:    0,
			RetryableStatus: []int{http.StatusServiceUnavailable},
			RetryIf: func(resp *http.Response, err error) bool {
				// Never retry regardless of status.
				return false
			},
		}),
	)

	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 (no retry), got %d", resp.StatusCode)
	}
	if srv.RequestCount() != 1 {
		t.Errorf("expected exactly 1 request (no retries), got %d", srv.RequestCount())
	}
}

func TestRetry_CustomRetryIfPredicateAllows(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	// 2 failures, then success.
	srv.Enqueue(testutil.MockResponse{Status: http.StatusForbidden}) // 403, not in default retryable set
	srv.Enqueue(testutil.MockResponse{Status: http.StatusForbidden})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(
		WithDisableCircuitBreaker(),
		WithRetry(&RetryConfig{
			MaxAttempts:     3,
			InitialInterval: 1 * time.Millisecond,
			MaxInterval:     5 * time.Millisecond,
			Multiplier:      1.0,
			RandomFactor:    0,
			RetryableStatus: []int{http.StatusForbidden}, // force 403 to be retryable
			RetryIf: func(resp *http.Response, err error) bool {
				return true
			},
		}),
	)

	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after retries, got %d", resp.StatusCode)
	}
	if srv.RequestCount() != 3 {
		t.Errorf("expected 3 requests, got %d", srv.RequestCount())
	}
}

func TestRetry_OnRetryCallbackFired(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	var callbackCount int32
	c := New(
		WithDisableCircuitBreaker(),
		WithRetry(&RetryConfig{
			MaxAttempts:     3,
			InitialInterval: 1 * time.Millisecond,
			MaxInterval:     5 * time.Millisecond,
			Multiplier:      1.0,
			RandomFactor:    0,
			RetryableStatus: []int{http.StatusInternalServerError},
			OnRetry: func(attempt int, resp *http.Response, err error) {
				atomic.AddInt32(&callbackCount, 1)
			},
		}),
	)

	_, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := atomic.LoadInt32(&callbackCount)
	if got != 2 {
		t.Errorf("expected OnRetry called 2 times, got %d", got)
	}
}

func TestWithDisableRetry_SingleAttemptOnly(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK}) // must not be reached

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 500 (no retry), got %d", resp.StatusCode)
	}
	if srv.RequestCount() != 1 {
		t.Errorf("expected 1 request, got %d", srv.RequestCount())
	}
}

func TestRetry_NetworkErrorIsRetried(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	// Connection error then success.
	srv.EnqueueError()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "retry-ok"})

	c := New(
		WithDisableCircuitBreaker(),
		WithRetry(&RetryConfig{
			MaxAttempts:     2,
			InitialInterval: 1 * time.Millisecond,
			MaxInterval:     5 * time.Millisecond,
			Multiplier:      1.0,
			RandomFactor:    0,
			RetryableStatus: []int{},
		}),
	)

	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after retry, got %d", resp.StatusCode)
	}
}
