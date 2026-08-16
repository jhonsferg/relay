//go:build docker

// Integration tests against a real Redis container, run via testcontainers-go.
// Opt-in (requires a local Docker daemon), skipped by default - run with
// `go test -tags=docker ./...`. The sliding-window limiter's correctness
// depends on the atomic Lua script in distributed.go executing exactly the
// way real Redis's EVAL does; miniredis (used by distributed_test.go)
// implements its own Lua interpreter that approximates but doesn't guarantee
// bug-for-bug identical semantics, so these tests re-run the core scenarios
// against the real thing.
package distributed_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	redisclient "github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	relaydist "github.com/jhonsferg/relay/ext/ratelimit/distributed"
)

// newDockerRedis starts a real Redis container and returns a client wired to
// it. The container is terminated when the test finishes.
func newDockerRedis(t *testing.T) *redisclient.Client {
	t.Helper()
	ctx := context.Background()

	container, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("tcredis.Run: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("container.Terminate: %v", err)
		}
	})

	connStr, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("ConnectionString: %v", err)
	}
	opts, err := redisclient.ParseURL(connStr)
	if err != nil {
		t.Fatalf("ParseURL(%q): %v", connStr, err)
	}

	rdb := redisclient.NewClient(opts)
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func TestDocker_Allow_WithinLimit(t *testing.T) {
	rdb := newDockerRedis(t)
	limiter := relaydist.New(rdb, "rl:docker:test", 5, time.Second)

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := limiter.Allow(ctx); err != nil {
			t.Fatalf("request %d: unexpected error: %v", i+1, err)
		}
	}
}

func TestDocker_Allow_ExceedsLimit(t *testing.T) {
	rdb := newDockerRedis(t)
	limiter := relaydist.New(rdb, "rl:docker:exceed", 3, time.Second)

	ctx := context.Background()
	for i := 0; i < 3; i++ {
		limiter.Allow(ctx) //nolint:errcheck - consume quota
	}

	err := limiter.Allow(ctx)
	if !errors.Is(err, relaydist.ErrRateLimited) {
		t.Errorf("error = %v, want ErrRateLimited", err)
	}
}

func TestDocker_Allow_WindowExpiry(t *testing.T) {
	rdb := newDockerRedis(t)
	limiter := relaydist.New(rdb, "rl:docker:expiry", 2, 500*time.Millisecond)

	ctx := context.Background()
	limiter.Allow(ctx) //nolint:errcheck
	limiter.Allow(ctx) //nolint:errcheck

	if err := limiter.Allow(ctx); !errors.Is(err, relaydist.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited before window expiry, got %v", err)
	}

	// Real Redis TTL is wall-clock, not fast-forwardable - sleep past it.
	time.Sleep(700 * time.Millisecond)

	if err := limiter.Allow(ctx); err != nil {
		t.Fatalf("expected allow after real window expiry, got %v", err)
	}
}

func TestDocker_Allow_ConcurrentRequests_SlidingWindowNotViolated(t *testing.T) {
	rdb := newDockerRedis(t)
	const limit = 10
	limiter := relaydist.New(rdb, "rl:docker:concurrent", limit, time.Second)

	ctx := context.Background()
	var wg sync.WaitGroup
	var allowed atomic.Int32

	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := limiter.Allow(ctx); err == nil {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()

	// This is the core correctness property of the atomic Lua script under
	// real concurrent access against a real Redis server - exactly what
	// miniredis's own Lua interpreter can't fully guarantee to reproduce.
	if int(allowed.Load()) > limit {
		t.Errorf("allowed %d requests, limit is %d - sliding window violated against real Redis", allowed.Load(), limit)
	}
}
