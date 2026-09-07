package relay_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/testutil"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHedging_FirstAttemptSucceeds(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: `"first"`})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: `"second"`})

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithHedgingN(50*time.Millisecond, 2),
	)

	result, _, err := relay.ExecuteAs[string](client, client.Get("/test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "first" {
		t.Errorf("expected 'first', got %q", result)
	}
}

func TestHedging_SecondAttemptWins(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{
		Status: http.StatusOK,
		Body:   `"slow"`,
		Delay:  200 * time.Millisecond,
	})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: `"fast"`})

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithHedgingN(10*time.Millisecond, 2),
	)

	result, _, err := relay.ExecuteAs[string](client, client.Get("/test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "fast" {
		t.Errorf("expected 'fast' (second attempt to win), got %q", result)
	}
}

func TestHedging_AllFail(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError, Body: `"err1"`})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError, Body: `"err2"`})

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithHedgingN(10*time.Millisecond, 2),
	)

	_, _, err := relay.ExecuteAs[string](client, client.Get("/test"))
	if err == nil {
		t.Error("expected an error but got none")
	}
}

// TestHedging_FastErrorDoesNotAbortRemainingAttempts guards against a fast
// transport-level error (err != nil, not merely a non-2xx status) from the
// first attempt short-circuiting the whole hedge and abandoning every
// remaining attempt - previously, receiving *any* result (success or
// failure) while waiting for the hedge delay returned immediately, instead
// of only returning early on success and otherwise falling through to
// launch the next attempt (matching the final collect loop's own
// only-succeed-early semantics).
func TestHedging_FastErrorDoesNotAbortRemainingAttempts(t *testing.T) {
	var calls atomic.Int32
	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		n := calls.Add(1)
		if n == 1 {
			// First attempt fails immediately with a real transport error -
			// no network call at all, so it always beats the hedge delay.
			return nil, errors.New("simulated fast connection failure")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`"second attempt won"`)),
			Request:    req,
		}, nil
	})

	client := relay.New(
		relay.WithBaseURL("http://example.invalid"),
		relay.WithTransportMiddleware(func(_ http.RoundTripper) http.RoundTripper { return base }),
		relay.WithHedgingN(10*time.Millisecond, 3),
		relay.WithDisableRetry(),
	)

	data, _, err := relay.ExecuteAs[string](client, client.Get("/test"))
	if err != nil {
		t.Fatalf("expected the second attempt to succeed, got error: %v", err)
	}
	if data != "second attempt won" {
		t.Errorf("data = %q, want %q", data, "second attempt won")
	}
	if calls.Load() < 2 {
		t.Errorf("expected at least 2 attempts to be launched, got %d - the fast first-attempt error aborted the remaining hedges", calls.Load())
	}
}

func TestHedging_ContextCancelled(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{
		Status: http.StatusOK,
		Body:   `"data"`,
		Delay:  500 * time.Millisecond,
	})

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithHedgingN(10*time.Millisecond, 3),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, _, err := relay.ExecuteAs[string](client, client.Get("/test").WithContext(ctx))
	if err == nil {
		t.Error("expected context cancellation error, got nil")
	}
}

func TestHedging_MaxAttempts(t *testing.T) {
	var callCount atomic.Int64

	srv := testutil.NewMockServer()
	defer srv.Close()

	// Slow responses ensure all hedge goroutines launch before any completes.
	for i := 0; i < 5; i++ {
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusInternalServerError,
			Body:   `"err"`,
			Delay:  200 * time.Millisecond,
		})
	}

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithHedgingN(1*time.Millisecond, 3),
	)
	client = client.With(
		relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
			return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				callCount.Add(1)
				return next.RoundTrip(req)
			})
		}),
	)

	_, _ = client.Execute(client.Get("/test"))

	if n := callCount.Load(); n != 3 {
		t.Errorf("expected exactly 3 attempts, got %d", n)
	}
}

// TestHedging_WinnerReturnsWithoutWaitingForSiblings guards against a
// regression where the "winner arrived while still waiting for the next
// hedge attempt's delay" path blocked synchronously on wg.Wait() for
// in-flight sibling attempts to notice context cancellation and unwind,
// before returning the winning response. That is the most common hedging
// outcome (most requests finish before the hedge delay even fires), so
// paying sibling-unwind latency there defeats the purpose of hedging
// (returning the fastest response as fast as possible).
func TestHedging_WinnerReturnsWithoutWaitingForSiblings(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: `"fast"`})

	const (
		hedgeAfter         = 10 * time.Millisecond
		winnerDelay        = 15 * time.Millisecond  // > hedgeAfter, so the sibling launches first
		siblingUnwindDelay = 150 * time.Millisecond // simulates a slow syscall that ignores ctx cancellation
	)

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithHedgingN(hedgeAfter, 3),
	)

	var attempts atomic.Int64
	client = client.With(
		relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
			return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				if attempts.Add(1) == 1 {
					time.Sleep(winnerDelay)
					return next.RoundTrip(req)
				}
				// Sibling attempts: simulate work that keeps running for a
				// while even after ctx is cancelled, regardless of ctx state.
				time.Sleep(siblingUnwindDelay)
				return nil, req.Context().Err()
			})
		}),
	)

	start := time.Now()
	result, _, err := relay.ExecuteAs[string](client, client.Get("/test"))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "fast" {
		t.Errorf("expected 'fast', got %q", result)
	}
	if elapsed >= siblingUnwindDelay/2 {
		t.Errorf("Execute took %v, which suggests it blocked waiting for a sibling to unwind (sibling unwind delay: %v)", elapsed, siblingUnwindDelay)
	}
}

func TestHedging_DisabledByDefault(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: `"ok"`})

	client := relay.New(relay.WithBaseURL(srv.URL()))

	result, _, err := relay.ExecuteAs[string](client, client.Get("/test"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "ok" {
		t.Errorf("expected 'ok', got %q", result)
	}
}
