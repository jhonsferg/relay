package relay_test

import (
	"context"
	"net/http"
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
