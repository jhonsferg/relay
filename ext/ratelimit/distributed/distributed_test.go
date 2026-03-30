package distributed_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/jhonsferg/relay"
	relaydist "github.com/jhonsferg/relay/ext/ratelimit/distributed"
)

func newRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, rdb
}

func TestAllow_WithinLimit(t *testing.T) {
	_, rdb := newRedis(t)
	limiter := relaydist.New(rdb, "rl:test", 5, time.Second)

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := limiter.Allow(ctx); err != nil {
			t.Fatalf("request %d: unexpected error: %v", i+1, err)
		}
	}
}

func TestAllow_ExceedsLimit(t *testing.T) {
	_, rdb := newRedis(t)
	limiter := relaydist.New(rdb, "rl:exceed", 3, time.Second)

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		limiter.Allow(ctx) //nolint:errcheck - consume quota
	}

	err := limiter.Allow(ctx)
	if err == nil {
		t.Fatal("expected ErrRateLimited, got nil")
	}
	if !errors.Is(err, relaydist.ErrRateLimited) {
		t.Errorf("error = %v, want ErrRateLimited", err)
	}
}

func TestAllow_WindowExpiry(t *testing.T) {
	mr, rdb := newRedis(t)
	limiter := relaydist.New(rdb, "rl:expiry", 2, 100*time.Millisecond)

	ctx := context.Background()
	limiter.Allow(ctx) //nolint:errcheck
	limiter.Allow(ctx) //nolint:errcheck

	// Window is full - should be rate limited.
	if err := limiter.Allow(ctx); !errors.Is(err, relaydist.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited before window expiry, got %v", err)
	}

	// Fast-forward past the window.
	mr.FastForward(150 * time.Millisecond)

	// Window has reset - should be allowed again.
	if err := limiter.Allow(ctx); err != nil {
		t.Fatalf("expected allow after window reset, got %v", err)
	}
}

func TestAllow_FailOpen_OnRedisError(t *testing.T) {
	// Use a Redis client pointing at a closed port - simulates Redis unavailability.
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	limiter := relaydist.New(rdb, "rl:failopen", 1, time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Should fail open (return nil) rather than blocking the caller.
	err := limiter.Allow(ctx)
	if err != nil {
		t.Errorf("expected fail-open (nil error), got %v", err)
	}
}

func TestWithRateLimit_IntegrationWithRelay(t *testing.T) {
	_, rdb := newRedis(t)

	var serverHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	limiter := relaydist.New(rdb, "rl:relay", 3, time.Second)
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relaydist.WithRateLimit(limiter),
	)

	var allowed, limited int
	for i := 0; i < 6; i++ {
		_, err := client.Execute(client.Get("/"))
		if errors.Is(err, relaydist.ErrRateLimited) {
			limited++
		} else if err == nil {
			allowed++
		}
	}

	if allowed != 3 {
		t.Errorf("allowed = %d, want 3", allowed)
	}
	if limited != 3 {
		t.Errorf("limited = %d, want 3", limited)
	}
	if serverHits.Load() != 3 {
		t.Errorf("server hits = %d, want 3 (rate-limited calls never reached server)", serverHits.Load())
	}
}

func TestWithRateLimit_IsolatedKeys(t *testing.T) {
	_, rdb := newRedis(t)

	// Two limiters with different keys - isolated quotas.
	l1 := relaydist.New(rdb, "rl:user:alice", 2, time.Second)
	l2 := relaydist.New(rdb, "rl:user:bob", 2, time.Second)

	ctx := context.Background()

	// Exhaust Alice's quota.
	l1.Allow(ctx) //nolint:errcheck
	l1.Allow(ctx) //nolint:errcheck
	if err := l1.Allow(ctx); !errors.Is(err, relaydist.ErrRateLimited) {
		t.Fatalf("alice: expected ErrRateLimited, got %v", err)
	}

	// Bob's quota is untouched.
	if err := l2.Allow(ctx); err != nil {
		t.Fatalf("bob: unexpected error: %v", err)
	}
}

func TestWithRateLimit_ConcurrentRequests(t *testing.T) {
	_, rdb := newRedis(t)
	const limit = 10
	limiter := relaydist.New(rdb, "rl:concurrent", limit, time.Second)

	ctx := context.Background()
	var wg sync.WaitGroup
	var allowed atomic.Int32

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := limiter.Allow(ctx); err == nil {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()

	if int(allowed.Load()) > limit {
		t.Errorf("allowed %d requests, limit is %d - sliding window violated", allowed.Load(), limit)
	}
}
