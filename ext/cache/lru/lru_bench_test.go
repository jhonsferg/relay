package lru_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/jhonsferg/relay"
	relaylru "github.com/jhonsferg/relay/ext/cache/lru"
)

// BenchmarkLRUCacheStore_GetHit measures the cost of a cache hit (the common
// case on the read path: Get + MoveToFront).
func BenchmarkLRUCacheStore_GetHit(b *testing.B) {
	c := relaylru.NewLRUCacheStore(1024)
	for i := 0; i < 1024; i++ {
		c.Set(fmt.Sprintf("key-%d", i), entry(200, 0))
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		c.Get(fmt.Sprintf("key-%d", i%1024))
	}
}

// BenchmarkLRUCacheStore_Set measures the cost of Set under steady-state
// eviction pressure (cache stays at capacity, every Set evicts the LRU
// entry).
func BenchmarkLRUCacheStore_Set(b *testing.B) {
	c := relaylru.NewLRUCacheStore(256)
	e := &relay.CachedResponse{
		StatusCode: 200,
		Status:     "200 OK",
		Headers:    http.Header{"X-Test": []string{"1"}},
		Body:       []byte("body"),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		c.Set(fmt.Sprintf("key-%d", i), e)
	}
}
