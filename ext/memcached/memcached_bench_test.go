package memcached_test

import (
	"testing"
	"time"

	relaymemcached "github.com/jhonsferg/relay/ext/memcached"
)

// BenchmarkCacheStore_Set measures CacheStore.Set's hot path: key encoding
// (base64, with SHA-256 fallback for long keys) plus JSON marshalling of the
// cached entry - work performed on every cacheable response.
func BenchmarkCacheStore_Set(b *testing.B) {
	fc := newFakeClient()
	store := relaymemcached.NewCacheStore(fc, "relay:bench:")
	entry := sampleEntry(time.Minute)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		store.Set("bench:key:https://api.example.com/v1/resource", entry)
	}
}

// BenchmarkCacheStore_Get measures CacheStore.Get's hot path: key encoding
// plus JSON unmarshalling - work performed on every request against a
// cache-enabled client.
func BenchmarkCacheStore_Get(b *testing.B) {
	fc := newFakeClient()
	store := relaymemcached.NewCacheStore(fc, "relay:bench:")
	store.Set("bench:key:https://api.example.com/v1/resource", sampleEntry(time.Hour))

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, ok := store.Get("bench:key:https://api.example.com/v1/resource"); !ok {
			b.Fatal("expected hit")
		}
	}
}
