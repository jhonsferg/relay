// Package lru provides a proper LRU (Least-Recently-Used) [relay.CacheStore]
// backed by a doubly-linked list. Access order is tracked so the least-recently
// accessed entry is evicted first when the cache reaches capacity.
//
// This is an improvement over the built-in [relay.NewInMemoryCacheStore], which
// uses insertion order (FIFO) eviction. Use this package when your workload has
// temporal locality - popular entries are accessed frequently and should stay
// in cache, while cold entries should be evicted first.
//
// Usage:
//
//	import (
//	    "github.com/jhonsferg/relay"
//	    relaylru "github.com/jhonsferg/relay/ext/cache/lru"
//	)
//
//	store := relaylru.NewLRUCacheStore(1024) // capacity: 1024 entries
//	client := relay.New(relay.WithCache(store))
//
// # Capacity
//
// Pass a capacity ≤ 0 to use the default of 256 entries.
//
// # Expiry
//
// Entries with a non-zero ExpiresAt are evicted lazily on access when their
// deadline has passed - no background goroutine is required.
//
// # Thread safety
//
// All methods are safe for concurrent use.
package lru

import (
	"container/list"
	"sync"
	"time"

	"github.com/jhonsferg/relay"
)

const defaultCapacity = 256

// lruEntry is the value stored in each list element.
type lruEntry struct {
	key  string
	resp *relay.CachedResponse
}

// LRUCacheStore is a [relay.CacheStore] with LRU eviction. The zero value is
// not usable; always construct via [NewLRUCacheStore].
type LRUCacheStore struct {
	mu   sync.Mutex
	cap  int
	list *list.List               // front = most recently used; back = least recently used
	idx  map[string]*list.Element // key → list element
}

// NewLRUCacheStore returns a thread-safe LRU cache that holds at most capacity
// entries. Pass capacity ≤ 0 to use the default (256 entries).
func NewLRUCacheStore(capacity int) *LRUCacheStore {
	if capacity <= 0 {
		capacity = defaultCapacity
	}
	return &LRUCacheStore{
		cap:  capacity,
		list: list.New(),
		idx:  make(map[string]*list.Element, capacity),
	}
}

// Get returns the cached entry for key if present and not expired.
// A cache hit moves the entry to the front (most-recently-used position).
// Returns nil, false on a miss or an expired entry.
func (c *LRUCacheStore) Get(key string) (*relay.CachedResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.idx[key]
	if !ok {
		return nil, false
	}

	e := el.Value.(*lruEntry)

	// Lazy expiry check.
	if !e.resp.ExpiresAt.IsZero() && time.Now().After(e.resp.ExpiresAt) {
		c.list.Remove(el)
		delete(c.idx, key)
		return nil, false
	}

	// Promote to MRU position.
	c.list.MoveToFront(el)
	return e.resp, true
}

// Set stores or replaces the entry for key. If the cache is at capacity the
// least-recently-used entry is evicted to make room.
func (c *LRUCacheStore) Set(key string, resp *relay.CachedResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Update in place if already present.
	if el, ok := c.idx[key]; ok {
		el.Value.(*lruEntry).resp = resp
		c.list.MoveToFront(el)
		return
	}

	// Evict LRU entry when at capacity.
	if c.list.Len() >= c.cap {
		back := c.list.Back()
		if back != nil {
			c.list.Remove(back)
			delete(c.idx, back.Value.(*lruEntry).key)
		}
	}

	// Insert new MRU entry.
	el := c.list.PushFront(&lruEntry{key: key, resp: resp})
	c.idx[key] = el
}

// Delete removes the entry for key. It is a no-op if the key is absent.
func (c *LRUCacheStore) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.idx[key]; ok {
		c.list.Remove(el)
		delete(c.idx, key)
	}
}

// Clear removes all entries from the cache.
func (c *LRUCacheStore) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.list.Init()
	c.idx = make(map[string]*list.Element, c.cap)
}

// Len returns the current number of entries in the cache.
func (c *LRUCacheStore) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.list.Len()
}

// Capacity returns the maximum number of entries the cache can hold.
func (c *LRUCacheStore) Capacity() int { return c.cap }
