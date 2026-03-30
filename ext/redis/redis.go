// Package redis provides a Redis-backed [relay.CacheStore] for the relay HTTP
// client. Cache entries are serialized as JSON blobs and stored with a TTL
// derived from each entry's ExpiresAt field. Keys are namespaced with a
// caller-supplied prefix so multiple clients can share one Redis instance
// without collisions.
//
// Usage:
//
//	import (
//	    "github.com/redis/go-redis/v9"
//	    "github.com/jhonsferg/relay"
//	    relayredis "github.com/jhonsferg/relay/ext/redis"
//	)
//
//	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
//	store := relayredis.NewCacheStore(rdb, "myapp:http-cache:")
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relay.WithCache(store),
//	)
//
// The rdb argument may be any type that satisfies [redis.Cmdable] - a
// *redis.Client, *redis.ClusterClient, *redis.Ring, etc.
//
// # TTL handling
//
// If a cached entry has a non-zero ExpiresAt, the Redis key is stored with
// the corresponding TTL so Redis itself evicts it. Entries with a zero
// ExpiresAt are stored without a TTL (persistent until evicted by Redis
// memory policy or an explicit [CacheStore.Delete] / [CacheStore.Clear]).
//
// # Cluster note
//
// [CacheStore.Clear] uses SCAN + DEL against a single connection. When rdb is
// a *redis.ClusterClient, SCAN only iterates keys on a single shard. Use
// per-shard ForEachMaster iteration in that case.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	redisclient "github.com/redis/go-redis/v9"

	"github.com/jhonsferg/relay"
)

// CacheStore is a [relay.CacheStore] backed by Redis.
// All methods are safe for concurrent use.
type CacheStore struct {
	rdb    redisclient.Cmdable
	prefix string
}

// NewCacheStore returns a Redis-backed [relay.CacheStore].
//
//   - rdb: any redis.Cmdable (*redis.Client, *redis.ClusterClient, …).
//   - prefix: string prepended to every cache key (e.g. "myapp:http-cache:").
//     An empty prefix is allowed but not recommended on shared Redis instances.
func NewCacheStore(rdb redisclient.Cmdable, prefix string) *CacheStore {
	return &CacheStore{rdb: rdb, prefix: prefix}
}

// cachedEntry is the JSON-serialisable representation of relay.CachedResponse.
// Using explicit JSON tags keeps the wire format stable even if relay's struct
// field names change in the future.
type cachedEntry struct {
	StatusCode   int         `json:"sc"`
	Status       string      `json:"st"`
	Headers      http.Header `json:"h"`
	Body         []byte      `json:"b"`
	ExpiresAt    time.Time   `json:"ea"`
	ETag         string      `json:"et,omitempty"`
	LastModified string      `json:"lm,omitempty"`
}

func marshal(r *relay.CachedResponse) ([]byte, error) {
	e := cachedEntry{
		StatusCode:   r.StatusCode,
		Status:       r.Status,
		Headers:      r.Headers,
		Body:         r.Body,
		ExpiresAt:    r.ExpiresAt,
		ETag:         r.ETag,
		LastModified: r.LastModified,
	}
	return json.Marshal(e)
}

func unmarshal(data []byte) (*relay.CachedResponse, error) {
	var e cachedEntry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	return &relay.CachedResponse{
		StatusCode:   e.StatusCode,
		Status:       e.Status,
		Headers:      e.Headers,
		Body:         e.Body,
		ExpiresAt:    e.ExpiresAt,
		ETag:         e.ETag,
		LastModified: e.LastModified,
	}, nil
}

// Get returns the cached entry for key if present and not expired.
// Returns nil, false on a Redis miss, a deserialization error, or an expired
// entry (the expired key is deleted proactively).
func (s *CacheStore) Get(key string) (*relay.CachedResponse, bool) {
	ctx := context.Background()
	data, err := s.rdb.Get(ctx, s.prefix+key).Bytes()
	if err != nil {
		if errors.Is(err, redisclient.Nil) {
			return nil, false
		}
		return nil, false
	}

	r, err := unmarshal(data)
	if err != nil {
		return nil, false
	}

	// Secondary expiry guard: relay may have a finer TTL resolution than
	// Redis (which truncates to whole seconds). Evict proactively if stale.
	if !r.ExpiresAt.IsZero() && time.Now().After(r.ExpiresAt) {
		s.rdb.Del(ctx, s.prefix+key) //nolint:errcheck
		return nil, false
	}

	return r, true
}

// Set stores or replaces the entry for key.
// If entry.ExpiresAt is non-zero, the Redis key is given the corresponding
// TTL. Already-expired entries are silently dropped.
func (s *CacheStore) Set(key string, entry *relay.CachedResponse) {
	var ttl time.Duration
	if !entry.ExpiresAt.IsZero() {
		ttl = time.Until(entry.ExpiresAt)
		if ttl <= 0 {
			return // already expired; do not store
		}
	}

	data, err := marshal(entry)
	if err != nil {
		return
	}

	s.rdb.Set(context.Background(), s.prefix+key, data, ttl) //nolint:errcheck
}

// Delete removes the entry for key. It is a no-op if the key is absent.
func (s *CacheStore) Delete(key string) {
	s.rdb.Del(context.Background(), s.prefix+key) //nolint:errcheck
}

// Clear removes all keys matching the store prefix using an iterative
// SCAN + DEL loop to avoid blocking the Redis server with a FLUSHDB.
//
// Safe for shared Redis instances: only keys with the configured prefix are
// removed. See the package-level documentation for cluster caveats.
func (s *CacheStore) Clear() {
	ctx := context.Background()
	pattern := s.prefix + "*"

	var cursor uint64
	for {
		keys, cur, err := s.rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return
		}
		if len(keys) > 0 {
			s.rdb.Del(ctx, keys...) //nolint:errcheck
		}
		cursor = cur
		if cursor == 0 {
			break
		}
	}
}
