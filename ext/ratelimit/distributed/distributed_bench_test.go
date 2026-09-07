package distributed_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	relaydist "github.com/jhonsferg/relay/ext/ratelimit/distributed"
)

// newBenchRedis is the benchmark equivalent of newRedis (which requires a
// *testing.T and so can't be reused directly from a *testing.B).
func newBenchRedis(b *testing.B) *redis.Client {
	b.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatalf("miniredis: %v", err)
	}
	b.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	b.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// BenchmarkAllow_WithinLimit measures the per-request cost of the sliding
// window Lua script when the limit is high enough that every call is
// allowed (the common case in production).
func BenchmarkAllow_WithinLimit(b *testing.B) {
	rdb := newBenchRedis(b)
	limiter := relaydist.New(rdb, "bench:rl", 1_000_000_000, time.Minute)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := limiter.Allow(ctx); err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
	}
}
