// Package main demonstrates how to implement relay's CacheStore interface with
// a custom backend. The example uses a simple in-memory store with comments
// showing exactly where Redis calls would go.
//
// To use a real Redis backend:
//  1. Add the driver: go get github.com/redis/go-redis/v9
//  2. Replace the in-memory store body below with redis.Client calls
//     (see the "Redis equivalent" comments throughout).
//  3. Wire it in identically: relay.WithCache(store)
//
// No Redis package is imported here — the example compiles with zero
// extra dependencies.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	relay "github.com/jhonsferg/relay"
)

// ---------------------------------------------------------------------------
// customCacheStore implements relay.CacheStore backed by a Go map.
//
// All four methods of the interface are required:
//   - Get(key string) (*relay.CachedResponse, bool)
//   - Set(key string, entry *relay.CachedResponse)
//   - Delete(key string)
//   - Clear()
// ---------------------------------------------------------------------------

// cacheItem wraps a CachedResponse with its serialised form so we can
// demonstrate what would be stored in Redis as a JSON blob.
type cacheItem struct {
	entry     *relay.CachedResponse
	expiresAt time.Time // zero = no expiry
}

type customCacheStore struct {
	mu    sync.RWMutex
	items map[string]*cacheItem

	// Redis equivalent field:
	//   rdb *redis.Client
	//   keyPrefix string
}

// newCustomCacheStore creates an empty cache store.
// Redis equivalent:
//
//	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
//	return &customCacheStore{rdb: rdb, keyPrefix: "relay:cache:"}
func newCustomCacheStore() *customCacheStore {
	return &customCacheStore{
		items: make(map[string]*cacheItem),
	}
}

// Get returns the cached entry for key if it exists and has not expired.
//
// Redis equivalent:
//
//	data, err := s.rdb.Get(ctx, s.keyPrefix+key).Bytes()
//	if errors.Is(err, redis.Nil) { return nil, false }
//	var entry relay.CachedResponse
//	json.Unmarshal(data, &entry)
//	return &entry, true
func (s *customCacheStore) Get(key string) (*relay.CachedResponse, bool) {
	s.mu.RLock()
	item, ok := s.items[key]
	s.mu.RUnlock()

	if !ok {
		return nil, false
	}
	// Lazy expiry: delete and miss if past deadline.
	if !item.expiresAt.IsZero() && time.Now().After(item.expiresAt) {
		s.Delete(key)
		return nil, false
	}
	return item.entry, true
}

// Set stores entry under key. If the entry has an ExpiresAt, we honour it.
//
// Redis equivalent:
//
//	data, _ := json.Marshal(entry)
//	ttl := time.Until(entry.ExpiresAt)
//	if ttl <= 0 { ttl = 0 } // 0 = no expiry in Redis SET EX
//	s.rdb.Set(ctx, s.keyPrefix+key, data, ttl)
func (s *customCacheStore) Set(key string, entry *relay.CachedResponse) {
	item := &cacheItem{entry: entry}
	if !entry.ExpiresAt.IsZero() {
		item.expiresAt = entry.ExpiresAt
	}

	// Demonstrate what would be serialised to Redis.
	if data, err := json.Marshal(entry); err == nil {
		_ = data // would be: s.rdb.Set(ctx, s.keyPrefix+key, data, ttl)
	}

	s.mu.Lock()
	s.items[key] = item
	s.mu.Unlock()
}

// Delete removes the entry for key. No-op when the key is absent.
//
// Redis equivalent:
//
//	s.rdb.Del(ctx, s.keyPrefix+key)
func (s *customCacheStore) Delete(key string) {
	s.mu.Lock()
	delete(s.items, key)
	s.mu.Unlock()
}

// Clear removes all entries. In a shared Redis cache you would want to
// use a key prefix + SCAN + DEL pattern rather than FLUSHDB.
//
// Redis equivalent (prefix scan):
//
//	var cursor uint64
//	for {
//	    keys, cur, _ := s.rdb.Scan(ctx, cursor, s.keyPrefix+"*", 100).Result()
//	    if len(keys) > 0 { s.rdb.Del(ctx, keys...) }
//	    if cur == 0 { break }
//	    cursor = cur
//	}
func (s *customCacheStore) Clear() {
	s.mu.Lock()
	s.items = make(map[string]*cacheItem)
	s.mu.Unlock()
}

// Size returns the number of live (non-expired) entries. Helper for the demo.
func (s *customCacheStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, item := range s.items {
		if item.expiresAt.IsZero() || time.Now().Before(item.expiresAt) {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------------------
// main: wire the store into a relay client and demonstrate cache behaviour.
// ---------------------------------------------------------------------------

func main() {
	// ---------------------------------------------------------------------------
	// Test server: responds with a Cache-Control max-age header so relay knows
	// the response is cacheable, and counts requests for verification.
	// ---------------------------------------------------------------------------
	var serverHits int
	mu := sync.Mutex{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		serverHits++
		hits := serverHits
		mu.Unlock()

		// Cache-Control: max-age=60 tells relay's caching transport to store
		// the response and serve it from cache for 60 seconds.
		w.Header().Set("Cache-Control", "max-age=60")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"hits":%d,"path":%q}`, hits, r.URL.Path)
	}))
	defer srv.Close()

	// ---------------------------------------------------------------------------
	// Build the relay client with our custom CacheStore.
	//
	// relay.WithCache(store) accepts any type that satisfies the CacheStore
	// interface — there is no dependency on the concrete type.
	// ---------------------------------------------------------------------------
	store := newCustomCacheStore()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		// Plug in the custom store. For Redis, this would be:
		//   relay.WithCache(newCustomCacheStore(rdb, "relay:cache:"))
		relay.WithCache(store),
		relay.WithDisableRetry(),
		relay.WithTimeout(5*time.Second),
	)

	// ---------------------------------------------------------------------------
	// First request: cache miss — goes to the server.
	// ---------------------------------------------------------------------------
	fmt.Println("=== Request 1: cache MISS (first request) ===")
	resp, err := client.Execute(client.Get("/products/42"))
	if err != nil {
		log.Fatalf("request 1 failed: %v", err)
	}
	fmt.Printf("  Status: %d   Body: %s\n", resp.StatusCode, resp.String())
	fmt.Printf("  Server hits: %d   Cache size: %d\n\n", serverHits, store.Size())

	// ---------------------------------------------------------------------------
	// Second request (same URL): cache HIT — served from the store.
	// ---------------------------------------------------------------------------
	fmt.Println("=== Request 2: cache HIT (same URL) ===")
	resp, err = client.Execute(client.Get("/products/42"))
	if err != nil {
		log.Fatalf("request 2 failed: %v", err)
	}
	fmt.Printf("  Status: %d   Body: %s\n", resp.StatusCode, resp.String())
	fmt.Printf("  Server hits: %d (unchanged)   Cache size: %d\n\n", serverHits, store.Size())

	// ---------------------------------------------------------------------------
	// Different URL: cache miss.
	// ---------------------------------------------------------------------------
	fmt.Println("=== Request 3: cache MISS (different path) ===")
	resp, err = client.Execute(client.Get("/products/99"))
	if err != nil {
		log.Fatalf("request 3 failed: %v", err)
	}
	fmt.Printf("  Status: %d   Body: %s\n", resp.StatusCode, resp.String())
	fmt.Printf("  Server hits: %d   Cache size: %d\n\n", serverHits, store.Size())

	// ---------------------------------------------------------------------------
	// Force bypass with Cache-Control: no-cache header.
	// relay's caching transport revalidates with the server on no-cache.
	// ---------------------------------------------------------------------------
	fmt.Println("=== Request 4: forced revalidation (no-cache) ===")
	resp, err = client.Execute(
		client.Get("/products/42").
			WithHeader("Cache-Control", "no-cache"),
	)
	if err != nil {
		log.Fatalf("request 4 failed: %v", err)
	}
	fmt.Printf("  Status: %d   Body: %s\n", resp.StatusCode, resp.String())
	fmt.Printf("  Server hits: %d   Cache size: %d\n\n", serverHits, store.Size())

	// ---------------------------------------------------------------------------
	// Manual cache operations: Delete and Clear.
	// ---------------------------------------------------------------------------
	fmt.Println("=== Manual cache manipulation ===")

	// Delete a specific entry (e.g., after a write invalidates the resource).
	store.Delete("GET:" + srv.URL + "/products/42")
	fmt.Printf("  After Delete: cache size = %d\n", store.Size())

	// Clear all entries (e.g., after a cache warm-up failure or config reload).
	store.Clear()
	fmt.Printf("  After Clear:  cache size = %d\n", store.Size())

	// Next request will be a cache miss again.
	resp, err = client.Execute(client.Get("/products/42"))
	if err != nil {
		log.Fatalf("post-clear request failed: %v", err)
	}
	fmt.Printf("  Post-clear status: %d   Server hits: %d\n", resp.StatusCode, serverHits)
}
