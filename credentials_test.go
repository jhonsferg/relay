package relay_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	relay "github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/testutil"
)

func TestStaticCredentialProvider_Bearer(t *testing.T) {
	t.Parallel()

	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	p := relay.StaticCredentialProvider(relay.Credentials{
		BearerToken: "my-secret-token",
	})
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithCredentialProvider(p),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	_, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	req, err := srv.TakeRequest(time.Second)
	if err != nil {
		t.Fatalf("TakeRequest: %v", err)
	}
	if got := req.Headers.Get("Authorization"); got != "Bearer my-secret-token" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer my-secret-token")
	}
}

func TestStaticCredentialProvider_Headers(t *testing.T) {
	t.Parallel()

	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	p := relay.StaticCredentialProvider(relay.Credentials{
		Headers: map[string]string{
			"X-API-Key": "key-abc123",
		},
	})
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithCredentialProvider(p),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	_, err := c.Execute(c.Get("/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	req, err := srv.TakeRequest(time.Second)
	if err != nil {
		t.Fatalf("TakeRequest: %v", err)
	}
	if got := req.Headers.Get("X-API-Key"); got != "key-abc123" {
		t.Errorf("X-API-Key = %q, want %q", got, "key-abc123")
	}
}

func TestRotatingTokenProvider_CachesToken(t *testing.T) {
	t.Parallel()

	var callCount int
	var mu sync.Mutex

	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	p := relay.NewRotatingTokenProvider(
		func(_ context.Context) (string, time.Time, error) {
			mu.Lock()
			defer mu.Unlock()
			callCount++
			return "cached-token", time.Now().Add(time.Hour), nil
		},
		5*time.Second,
	)
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithCredentialProvider(p),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	// Two sequential requests should trigger only one refresh.
	for i := 0; i < 2; i++ {
		if _, err := c.Execute(c.Get("/")); err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
		if _, err := srv.TakeRequest(time.Second); err != nil {
			t.Fatalf("TakeRequest %d: %v", i, err)
		}
	}

	mu.Lock()
	got := callCount
	mu.Unlock()
	if got != 1 {
		t.Errorf("refresh called %d times, want 1", got)
	}
}

func TestRotatingTokenProvider_RefreshesOnExpiry(t *testing.T) {
	t.Parallel()

	var callCount int
	var mu sync.Mutex

	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	p := relay.NewRotatingTokenProvider(
		func(_ context.Context) (string, time.Time, error) {
			mu.Lock()
			defer mu.Unlock()
			callCount++
			// Return a token that is immediately within the threshold window.
			return "refreshed-token", time.Now().Add(time.Millisecond), nil
		},
		time.Second, // threshold is 1 s, token expires in 1 ms → always refreshed
	)
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithCredentialProvider(p),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	for i := 0; i < 2; i++ {
		if _, err := c.Execute(c.Get("/")); err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
		if _, err := srv.TakeRequest(time.Second); err != nil {
			t.Fatalf("TakeRequest %d: %v", i, err)
		}
	}

	mu.Lock()
	got := callCount
	mu.Unlock()
	if got < 2 {
		t.Errorf("refresh called %d times, want >= 2", got)
	}
}

func TestWithCredentialProvider_Integration(t *testing.T) {
	t.Parallel()

	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})

	p := relay.StaticCredentialProvider(relay.Credentials{
		BearerToken: "integration-token",
		Headers: map[string]string{
			"X-Request-Source": "relay-test",
		},
	})
	c := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithCredentialProvider(p),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	resp, err := c.Execute(c.Get("/endpoint"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}

	req, err := srv.TakeRequest(time.Second)
	if err != nil {
		t.Fatalf("TakeRequest: %v", err)
	}
	if got := req.Headers.Get("Authorization"); got != "Bearer integration-token" {
		t.Errorf("Authorization = %q, want %q", got, "Bearer integration-token")
	}
	if got := req.Headers.Get("X-Request-Source"); got != "relay-test" {
		t.Errorf("X-Request-Source = %q, want %q", got, "relay-test")
	}
}
