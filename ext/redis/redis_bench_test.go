package redis_test

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redisclient "github.com/redis/go-redis/v9"

	relayredis "github.com/jhonsferg/relay/ext/redis"
)

// newBenchStore is the benchmark equivalent of newTestStore (which requires
// a *testing.T and so can't be reused directly from a *testing.B).
func newBenchStore(b *testing.B) *relayredis.CacheStore {
	b.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatalf("miniredis.Run: %v", err)
	}
	b.Cleanup(mr.Close)

	rdb := redisclient.NewClient(&redisclient.Options{Addr: mr.Addr()})
	b.Cleanup(func() { _ = rdb.Close() })

	return relayredis.NewCacheStore(rdb, "relay:bench:")
}

// BenchmarkCacheStore_Set measures the per-request cost of serializing and
// writing a cache entry to Redis.
func BenchmarkCacheStore_Set(b *testing.B) {
	store := newBenchStore(b)
	entry := sampleEntry(time.Minute)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		store.Set("bench-key", entry)
	}
}

// BenchmarkCacheStore_Get measures the per-request cost of reading and
// deserializing a cache entry from Redis (cache hit path).
func BenchmarkCacheStore_Get(b *testing.B) {
	store := newBenchStore(b)
	entry := sampleEntry(time.Minute)
	store.Set("bench-key", entry)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, ok := store.Get("bench-key")
		if !ok {
			b.Fatal("expected cache hit")
		}
	}
}
