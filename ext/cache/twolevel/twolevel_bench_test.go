package twolevel_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/jhonsferg/relay"
	relaylru "github.com/jhonsferg/relay/ext/cache/lru"
	relaytwo "github.com/jhonsferg/relay/ext/cache/twolevel"
)

// BenchmarkTwoLevelCacheStore_L1Hit measures the fast path: every Get is
// served directly from L1 (the common case in steady-state traffic).
func BenchmarkTwoLevelCacheStore_L1Hit(b *testing.B) {
	l1 := relaylru.NewLRUCacheStore(1024)
	l2 := newFake()
	store := relaytwo.New(l1, l2)

	e := &relay.CachedResponse{StatusCode: 200, Status: "200 OK", Body: []byte("body")}
	for i := 0; i < 1024; i++ {
		store.Set(fmt.Sprintf("key-%d", i), e)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		store.Get(fmt.Sprintf("key-%d", i%1024))
	}
}

// BenchmarkTwoLevelCacheStore_L2BackFill measures the slower path: every Get
// misses L1 and must fall through to L2, then back-fill L1.
func BenchmarkTwoLevelCacheStore_L2BackFill(b *testing.B) {
	l1 := relaylru.NewLRUCacheStore(1024)
	l2 := newFake()
	store := relaytwo.New(l1, l2)

	e := &relay.CachedResponse{StatusCode: 200, Status: "200 OK", Body: []byte("body")}
	l2.Set("k", e)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		l1.Delete("k") // force an L1 miss every iteration
		store.Get("k")
	}
}

// BenchmarkTwoLevelCacheStore_Set measures the write path, which fans out to
// both L1 and L2 on every call.
func BenchmarkTwoLevelCacheStore_Set(b *testing.B) {
	l1 := relaylru.NewLRUCacheStore(1024)
	l2 := newFake()
	store := relaytwo.New(l1, l2)
	e := &relay.CachedResponse{StatusCode: 200, Status: "200 OK", Headers: http.Header{}, Body: []byte("body")}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		store.Set(fmt.Sprintf("key-%d", i%1024), e)
	}
}
