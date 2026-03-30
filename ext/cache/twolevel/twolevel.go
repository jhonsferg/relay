// Package twolevel provides a two-level [relay.CacheStore] that combines a
// fast L1 cache (typically in-memory) with a slower but larger/persistent L2
// cache (typically Redis or Memcached).
//
// Read path:
//  1. Check L1. On hit, return immediately (sub-microsecond for in-memory L1).
//  2. Check L2 on L1 miss. On hit, back-fill L1 and return.
//  3. Return miss if both levels miss.
//
// Write path:
//   - Write to both L1 and L2 simultaneously.
//
// Delete / Clear:
//   - Applied to both levels.
//
// Usage:
//
//	import (
//	    "github.com/jhonsferg/relay"
//	    relaylru "github.com/jhonsferg/relay/ext/cache/lru"
//	    relayredis "github.com/jhonsferg/relay/ext/redis"
//	    relaytwo "github.com/jhonsferg/relay/ext/cache/twolevel"
//	)
//
//	l1 := relaylru.NewLRUCacheStore(256)
//	l2 := relayredis.NewCacheStore(rdb, "myapp:http:")
//	store := relaytwo.New(l1, l2)
//
//	client := relay.New(relay.WithCache(store))
//
// # Thread safety
//
// All methods are safe for concurrent use as long as the underlying L1 and L2
// stores are themselves thread-safe.
package twolevel

import "github.com/jhonsferg/relay"

// TwoLevelCacheStore combines an L1 (fast, small) and L2 (slow, large)
// [relay.CacheStore]. On an L1 miss the L2 is consulted and the result is
// promoted to L1.
type TwoLevelCacheStore struct {
	l1 relay.CacheStore
	l2 relay.CacheStore
}

// New returns a two-level cache. Both l1 and l2 must be non-nil.
// l1 is queried first (fast path); l2 is the fallback (slower, persistent).
func New(l1, l2 relay.CacheStore) *TwoLevelCacheStore {
	if l1 == nil {
		panic("twolevel: l1 must not be nil")
	}
	if l2 == nil {
		panic("twolevel: l2 must not be nil")
	}
	return &TwoLevelCacheStore{l1: l1, l2: l2}
}

// Get looks up key in L1 first. On a miss it falls through to L2 and, on a
// hit, back-fills L1 before returning the entry.
func (c *TwoLevelCacheStore) Get(key string) (*relay.CachedResponse, bool) {
	if resp, ok := c.l1.Get(key); ok {
		return resp, true
	}
	resp, ok := c.l2.Get(key)
	if !ok {
		return nil, false
	}
	// Back-fill L1 so subsequent reads are served from the fast cache.
	c.l1.Set(key, resp)
	return resp, true
}

// Set writes entry to both L1 and L2.
func (c *TwoLevelCacheStore) Set(key string, entry *relay.CachedResponse) {
	c.l1.Set(key, entry)
	c.l2.Set(key, entry)
}

// Delete removes key from both L1 and L2.
func (c *TwoLevelCacheStore) Delete(key string) {
	c.l1.Delete(key)
	c.l2.Delete(key)
}

// Clear removes all entries from both L1 and L2.
// See the documentation of each underlying store for caveats (e.g. the Redis
// store's Clear uses SCAN+DEL to avoid FLUSHDB on shared instances).
func (c *TwoLevelCacheStore) Clear() {
	c.l1.Clear()
	c.l2.Clear()
}
