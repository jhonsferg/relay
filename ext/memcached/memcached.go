// Package memcached provides a Memcached-backed [relay.CacheStore] for the
// relay HTTP client using github.com/bradfitz/gomemcache.
//
// Usage:
//
//	import (
//	    "github.com/bradfitz/gomemcache/memcache"
//	    "github.com/jhonsferg/relay"
//	    relaymemcached "github.com/jhonsferg/relay/ext/memcached"
//	)
//
//	mc := memcache.New("localhost:11211")
//	store := relaymemcached.NewCacheStore(mc, "myapp:http:")
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relay.WithCache(store),
//	)
//
// # Key format
//
// Cache keys are prefixed with the string passed to [NewCacheStore] and then
// URL-safe base64-encoded so they satisfy memcached's key constraints (no
// spaces, no control characters, max 250 bytes).
//
// # TTL handling
//
// If a cached entry has a non-zero ExpiresAt, the memcached item is stored
// with the corresponding TTL in seconds (minimum 1 s). Items with a zero
// ExpiresAt are stored with expiration 0, meaning they never expire unless
// evicted by the server's memory pressure (LRU).
//
// # Clear behavior
//
// [CacheStore.Clear] calls the underlying client's FlushAll which purges the
// entire server. If this CacheStore shares a memcached instance with other
// applications, prefer calling [CacheStore.Delete] on specific keys instead.
// For production multi-tenant setups, use a dedicated memcached instance per
// application or migrate to the Redis backend (ext/redis) which supports
// prefix-scoped Clear.
//
// # Client interface
//
// NewCacheStore accepts a [Client] interface rather than the concrete
// *memcache.Client, making it easy to swap in a test double without needing
// a real memcached daemon.
package memcached

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/bradfitz/gomemcache/memcache"

	"github.com/jhonsferg/relay"
)

// Client is the subset of *memcache.Client used by CacheStore. Implement
// this interface for test doubles or alternative Memcached client libraries.
//
// The concrete *memcache.Client from github.com/bradfitz/gomemcache/memcache
// satisfies this interface automatically.
type Client interface {
	Get(key string) (*memcache.Item, error)
	Set(item *memcache.Item) error
	Delete(key string) error
	FlushAll() error
}

// CacheStore is a [relay.CacheStore] backed by Memcached.
// All methods are safe for concurrent use.
type CacheStore struct {
	mc     Client
	prefix string
}

// NewCacheStore returns a Memcached-backed [relay.CacheStore].
//
//   - mc: a *memcache.Client (or any value satisfying [Client]).
//   - prefix: string prepended to every cache key before encoding.
//     Use a short, unique prefix per application to avoid key collisions when
//     a shared Memcached instance is used.
func NewCacheStore(mc Client, prefix string) *CacheStore {
	return &CacheStore{mc: mc, prefix: prefix}
}

// cachedEntry is the JSON-serialisable representation of relay.CachedResponse.
type cachedEntry struct {
	StatusCode   int         `json:"sc"`
	Status       string      `json:"st"`
	Headers      http.Header `json:"h"`
	Body         []byte      `json:"b"`
	ExpiresAt    time.Time   `json:"ea"`
	ETag         string      `json:"et,omitempty"`
	LastModified string      `json:"lm,omitempty"`
}

// encodeKey converts a relay cache key to a memcached-safe key:
//   - Prepend the configured prefix.
//   - Base64-encode (URL-safe, no padding) to eliminate spaces and control chars.
//   - Truncate to 250 bytes (memcached limit).
func (s *CacheStore) encodeKey(key string) string {
	raw := s.prefix + key
	enc := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(raw))
	if len(enc) > 250 {
		enc = enc[:250]
	}
	return enc
}

func marshalEntry(r *relay.CachedResponse) ([]byte, error) {
	return json.Marshal(cachedEntry{
		StatusCode:   r.StatusCode,
		Status:       r.Status,
		Headers:      r.Headers,
		Body:         r.Body,
		ExpiresAt:    r.ExpiresAt,
		ETag:         r.ETag,
		LastModified: r.LastModified,
	})
}

func unmarshalEntry(data []byte) (*relay.CachedResponse, error) {
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

// Get returns the cached entry for key if it exists and has not expired.
// Returns nil, false on a cache miss, a deserialisation error, or when the
// entry's ExpiresAt has passed (secondary guard against sub-second precision
// differences between relay and memcached TTLs).
func (s *CacheStore) Get(key string) (*relay.CachedResponse, bool) {
	item, err := s.mc.Get(s.encodeKey(key))
	if err != nil {
		if errors.Is(err, memcache.ErrCacheMiss) {
			return nil, false
		}
		return nil, false
	}

	r, err := unmarshalEntry(item.Value)
	if err != nil {
		return nil, false
	}

	if !r.ExpiresAt.IsZero() && time.Now().After(r.ExpiresAt) {
		s.mc.Delete(s.encodeKey(key)) //nolint:errcheck
		return nil, false
	}

	return r, true
}

// Set stores or replaces the entry for key.
// If entry.ExpiresAt is non-zero, the memcached item TTL is set to
// ceil(seconds until ExpiresAt). Already-expired entries are silently dropped.
func (s *CacheStore) Set(key string, entry *relay.CachedResponse) {
	var expSecs int32
	if !entry.ExpiresAt.IsZero() {
		d := time.Until(entry.ExpiresAt)
		if d <= 0 {
			return // already expired; do not store
		}
		secs := int32(d.Seconds())
		if secs == 0 {
			secs = 1 // at least 1 s to avoid immediate eviction
		}
		expSecs = secs
	}

	data, err := marshalEntry(entry)
	if err != nil {
		return
	}

	s.mc.Set(&memcache.Item{ //nolint:errcheck
		Key:        s.encodeKey(key),
		Value:      data,
		Expiration: expSecs,
	})
}

// Delete removes the entry for key. It is a no-op if the key is absent.
func (s *CacheStore) Delete(key string) {
	err := s.mc.Delete(s.encodeKey(key))
	if err != nil && !errors.Is(err, memcache.ErrCacheMiss) {
		// Ignore; delete is best-effort.
	}
}

// Clear flushes the entire Memcached server. See the package documentation
// for caveats about shared Memcached instances.
func (s *CacheStore) Clear() {
	s.mc.FlushAll() //nolint:errcheck
}
