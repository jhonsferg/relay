package relay

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CachedResponse holds a serialised HTTP response for replay. It is stored by
// [cachingTransport] after a cache-eligible response is received and returned
// on subsequent matching requests until the entry expires.
type CachedResponse struct {
	// StatusCode is the HTTP status code of the original response.
	StatusCode int

	// Status is the human-readable status line (e.g. "200 OK").
	Status string

	// Headers is a clone of the original response headers.
	Headers http.Header

	// Body is the full response body bytes.
	Body []byte

	// ExpiresAt is the absolute time after which this entry must not be served.
	// A zero value means the entry has no expiry derived from the response.
	ExpiresAt time.Time

	// ETag is the value of the ETag response header, used for conditional
	// revalidation via If-None-Match on subsequent requests.
	ETag string

	// LastModified is the value of the Last-Modified response header, used for
	// conditional revalidation via If-Modified-Since when ETag is absent.
	LastModified string
}

// isExpired reports whether the entry has passed its ExpiresAt deadline.
// Entries with a zero ExpiresAt are considered non-expiring.
func (e *CachedResponse) isExpired() bool {
	return !e.ExpiresAt.IsZero() && time.Now().After(e.ExpiresAt)
}

// CacheStore is the storage backend for the HTTP response cache. Implement
// this interface to plug in Redis, Memcached, disk, or any other store. All
// methods must be safe for concurrent use.
type CacheStore interface {
	// Get returns the cached entry for key and true if found, or nil and false
	// if the key is absent or the entry has expired.
	Get(key string) (*CachedResponse, bool)

	// Set stores or replaces the entry for key.
	Set(key string, entry *CachedResponse)

	// Delete removes the entry for key. It is a no-op if the key is absent.
	Delete(key string)

	// Clear removes all entries from the store.
	Clear()
}

// inMemoryCacheStore is a thread-safe in-memory cache with insertion-order
// eviction. Expired entries are always preferred for eviction over live ones.
type inMemoryCacheStore struct {
	// mu protects entries and insertOrder.
	mu sync.Mutex

	// entries maps cache key to cached response.
	entries map[string]*CachedResponse

	// insertOrder records the insertion sequence of keys. It is used to evict
	// the oldest entry when the store is at capacity. It is compacted during
	// eviction to remove stale references.
	insertOrder []string

	// maxEntries is the capacity of the store.
	maxEntries int
}

// NewInMemoryCacheStore returns an in-memory [CacheStore] with the given
// capacity. When at capacity, expired entries are evicted first; then the
// oldest by insertion order. Passing maxEntries <= 0 defaults to 256.
func NewInMemoryCacheStore(maxEntries int) CacheStore {
	if maxEntries <= 0 {
		maxEntries = 256
	}
	return &inMemoryCacheStore{
		entries:    make(map[string]*CachedResponse, maxEntries),
		maxEntries: maxEntries,
	}
}

// Get returns the entry for key if present and not expired. Expired entries
// are deleted on access (lazy expiration).
func (s *inMemoryCacheStore) Get(key string) (*CachedResponse, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		return nil, false
	}
	if e.isExpired() {
		delete(s.entries, key)
		return nil, false
	}
	return e, true
}

// Set stores entry under key. If the key already exists the entry is updated
// in-place without growing insertOrder. New keys trigger eviction when the
// store is at capacity.
func (s *inMemoryCacheStore) Set(key string, entry *CachedResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.entries[key]; !exists {
		if len(s.entries) >= s.maxEntries {
			s.evict()
		}
		s.insertOrder = append(s.insertOrder, key)
	}
	s.entries[key] = entry
}

// evict removes expired entries first, then the oldest by insertion order,
// until len(entries) drops below maxEntries. It also compacts insertOrder to
// remove stale references left from previous deletions.
func (s *inMemoryCacheStore) evict() {
	// Pass 1: scan for and remove all expired entries.
	expired := make(map[string]struct{})
	for k, e := range s.entries {
		if e.isExpired() {
			delete(s.entries, k)
			expired[k] = struct{}{}
		}
	}

	// Compact insertOrder, removing keys that were just expired.
	if len(expired) > 0 {
		i := 0
		for _, k := range s.insertOrder {
			if _, wasExpired := expired[k]; !wasExpired {
				s.insertOrder[i] = k
				i++
			}
		}
		s.insertOrder = s.insertOrder[:i]
	}

	// Pass 2: if still at capacity, evict the oldest entries by insertion order.
	for len(s.entries) >= s.maxEntries && len(s.insertOrder) > 0 {
		oldest := s.insertOrder[0]
		s.insertOrder = s.insertOrder[1:]
		delete(s.entries, oldest)
	}
}

// Delete removes the entry for key. It is a no-op if the key is not present.
func (s *inMemoryCacheStore) Delete(key string) {
	s.mu.Lock()
	delete(s.entries, key)
	s.mu.Unlock()
}

// Clear removes all entries from the store and resets the insertion-order list.
func (s *inMemoryCacheStore) Clear() {
	s.mu.Lock()
	s.entries = make(map[string]*CachedResponse, s.maxEntries)
	s.insertOrder = s.insertOrder[:0]
	s.mu.Unlock()
}

// cachingTransport is an [http.RoundTripper] that caches GET and HEAD
// responses according to RFC 7234. It respects Cache-Control (no-store,
// no-cache, max-age), Expires, ETag/If-None-Match, and
// Last-Modified/If-Modified-Since for conditional revalidation.
type cachingTransport struct {
	// base is the next transport in the stack, used for cache misses.
	base http.RoundTripper

	// store is the backing cache storage.
	store CacheStore
}

// newCachingTransport wraps base with a caching layer backed by store.
func newCachingTransport(base http.RoundTripper, store CacheStore) http.RoundTripper {
	return &cachingTransport{base: base, store: store}
}

// RoundTrip serves from cache when possible, revalidates with conditional
// requests when a cached entry exists, and stores cacheable origin responses.
func (t *cachingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return t.base.RoundTrip(req)
	}
	if strings.Contains(req.Header.Get("Cache-Control"), "no-store") {
		return t.base.RoundTrip(req)
	}

	key := req.Method + ":" + req.URL.String()
	cached, hasCached := t.store.Get(key)
	forceRevalidate := strings.Contains(req.Header.Get("Cache-Control"), "no-cache")

	if hasCached && !forceRevalidate {
		return replayResponse(req, cached), nil
	}

	// Add conditional request headers for revalidation when we have a cached entry.
	if hasCached {
		if cached.ETag != "" {
			req.Header.Set("If-None-Match", cached.ETag)
		} else if cached.LastModified != "" {
			req.Header.Set("If-Modified-Since", cached.LastModified)
		}
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// 304 Not Modified — serve the existing cached entry without re-storing.
	if resp.StatusCode == http.StatusNotModified {
		_ = resp.Body.Close() //nolint:errcheck
		if hasCached {
			return replayResponse(req, cached), nil
		}
	}

	// Store only 200 GET responses that are not marked private or no-store.
	cc := resp.Header.Get("Cache-Control")
	if resp.StatusCode == http.StatusOK &&
		!strings.Contains(cc, "no-store") &&
		!strings.Contains(cc, "private") {
		if entry := buildCacheEntry(resp); entry != nil {
			t.store.Set(key, entry)
			resp.Body = io.NopCloser(bytes.NewReader(entry.Body))
		}
	}

	return resp, nil
}

// buildCacheEntry reads the response body and constructs a [CachedResponse].
// It parses Cache-Control max-age and Expires to set ExpiresAt. Returns nil if
// the body cannot be read.
func buildCacheEntry(resp *http.Response) *CachedResponse {
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close() //nolint:errcheck
	if err != nil {
		return nil
	}

	entry := &CachedResponse{
		StatusCode:   resp.StatusCode,
		Status:       resp.Status,
		Headers:      resp.Header.Clone(),
		Body:         body,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}

	cc := resp.Header.Get("Cache-Control")
	if s := parseMaxAge(cc); s > 0 {
		entry.ExpiresAt = time.Now().Add(time.Duration(s) * time.Second)
	} else if exp := resp.Header.Get("Expires"); exp != "" {
		if t, err := http.ParseTime(exp); err == nil {
			entry.ExpiresAt = t
		}
	}

	return entry
}

// replayResponse constructs a synthetic *http.Response from a cached entry,
// re-associating it with the originating request.
func replayResponse(req *http.Request, c *CachedResponse) *http.Response {
	return &http.Response{
		StatusCode: c.StatusCode,
		Status:     c.Status,
		Header:     c.Headers.Clone(),
		Body:       io.NopCloser(bytes.NewReader(c.Body)),
		Request:    req,
	}
}

// parseMaxAge extracts the max-age value (in seconds) from a Cache-Control
// header string. Returns 0 if the directive is absent or unparseable.
func parseMaxAge(cacheControl string) int {
	for _, part := range strings.Split(cacheControl, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "max-age=") {
			if n, err := strconv.Atoi(strings.TrimPrefix(part, "max-age=")); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}
