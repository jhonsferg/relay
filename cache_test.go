package relay

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

func TestCache_HitSecondRequestDoesNotReachServer(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{
		Status: http.StatusOK,
		Headers: map[string]string{
			"Cache-Control": "max-age=3600",
			"Content-Type":  "text/plain",
		},
		Body: "cached-body",
	})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithInMemoryCache(64))

	// First request: cache miss → hits server.
	resp1, err := c.Execute(c.Get(srv.URL() + "/resource"))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if resp1.String() != "cached-body" {
		t.Errorf("first response body: expected 'cached-body', got %q", resp1.String())
	}
	if srv.RequestCount() != 1 {
		t.Errorf("expected 1 server request after first call, got %d", srv.RequestCount())
	}

	// Second request: cache hit → server should NOT be called again.
	resp2, err := c.Execute(c.Get(srv.URL() + "/resource"))
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if resp2.String() != "cached-body" {
		t.Errorf("second response body: expected 'cached-body', got %q", resp2.String())
	}
	if srv.RequestCount() != 1 {
		t.Errorf("expected 1 server request (cache hit), got %d", srv.RequestCount())
	}
}

func TestCache_NoStoreMissesCache(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	// Two responses with Cache-Control: no-store - never cached.
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Cache-Control": "no-store"},
		Body:    "no-store-1",
	})
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Cache-Control": "no-store"},
		Body:    "no-store-2",
	})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithInMemoryCache(64))

	_, err := c.Execute(c.Get(srv.URL() + "/ns"))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	_, err = c.Execute(c.Get(srv.URL() + "/ns"))
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	if srv.RequestCount() != 2 {
		t.Errorf("Cache-Control: no-store should bypass cache; expected 2 server requests, got %d", srv.RequestCount())
	}
}

func TestCache_RequestNoCacheBypassesCache(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Cache-Control": "max-age=3600"},
		Body:    "first",
	})
	// Second response served after forced revalidation.
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Cache-Control": "max-age=3600"},
		Body:    "second",
	})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithInMemoryCache(64))

	// Prime the cache.
	_, _ = c.Execute(c.Get(srv.URL() + "/revalidate"))

	// Second request with Cache-Control: no-cache forces revalidation.
	req := c.Get(srv.URL()+"/revalidate").WithHeader("Cache-Control", "no-cache")
	resp, err := c.Execute(req)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if srv.RequestCount() != 2 {
		t.Errorf("expected 2 server requests (no-cache forces revalidation), got %d", srv.RequestCount())
	}
	_ = resp
}

func TestCache_ETagRevalidation304(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	// First response: cacheable with ETag and a long max-age so the entry is stored.
	srv.Enqueue(testutil.MockResponse{
		Status: http.StatusOK,
		Headers: map[string]string{
			"Cache-Control": "max-age=3600",
			"ETag":          `"abc123"`,
			"Content-Type":  "text/plain",
		},
		Body: "original-body",
	})
	// Second response: 304 Not Modified (ETag matched).
	srv.Enqueue(testutil.MockResponse{
		Status: http.StatusNotModified,
	})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithInMemoryCache(64))

	// First request: cache miss → stores the entry.
	resp1, err := c.Execute(c.Get(srv.URL() + "/etag"))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if resp1.String() != "original-body" {
		t.Errorf("expected original body, got %q", resp1.String())
	}

	// Second request: force revalidation via request Cache-Control: no-cache.
	// This causes the caching transport to send If-None-Match.
	req2 := c.Get(srv.URL()+"/etag").WithHeader("Cache-Control", "no-cache")
	resp2, err := c.Execute(req2)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	// Server returns 304 → client should serve the cached body.
	if resp2.String() != "original-body" {
		t.Errorf("304 revalidation should serve cached body, got %q", resp2.String())
	}
	if srv.RequestCount() != 2 {
		t.Errorf("expected 2 server requests (initial + revalidation), got %d", srv.RequestCount())
	}

	// Verify If-None-Match was sent on second request.
	srv.TakeRequest(time.Second) //nolint:errcheck
	recorded2, err := srv.TakeRequest(time.Second)
	if err != nil {
		t.Fatalf("second recorded request not found: %v", err)
	}
	if recorded2.Headers.Get("If-None-Match") == "" {
		t.Errorf("expected If-None-Match header on revalidation request")
	}
}

func TestCache_LastModifiedRevalidation304(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	lastModified := "Wed, 21 Oct 2015 07:28:00 GMT"

	// First response with Last-Modified, no ETag, stored via max-age.
	srv.Enqueue(testutil.MockResponse{
		Status: http.StatusOK,
		Headers: map[string]string{
			"Cache-Control": "max-age=3600",
			"Last-Modified": lastModified,
			"Content-Type":  "text/plain",
		},
		Body: "lm-body",
	})
	// Second: 304 Not Modified.
	srv.Enqueue(testutil.MockResponse{
		Status: http.StatusNotModified,
	})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithInMemoryCache(64))

	// First request: cache miss → stores the entry.
	resp1, err := c.Execute(c.Get(srv.URL() + "/lastmod"))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if resp1.String() != "lm-body" {
		t.Errorf("expected 'lm-body', got %q", resp1.String())
	}

	// Second request: force revalidation via request Cache-Control: no-cache.
	req2 := c.Get(srv.URL()+"/lastmod").WithHeader("Cache-Control", "no-cache")
	resp2, err := c.Execute(req2)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if resp2.String() != "lm-body" {
		t.Errorf("304 revalidation should serve cached body, got %q", resp2.String())
	}

	// Check that If-Modified-Since was sent.
	srv.TakeRequest(time.Second) //nolint:errcheck
	recorded2, err := srv.TakeRequest(time.Second)
	if err != nil {
		t.Fatalf("second recorded request not found: %v", err)
	}
	if recorded2.Headers.Get("If-Modified-Since") == "" {
		t.Errorf("expected If-Modified-Since header on revalidation request")
	}
}

func TestCache_LRUEviction(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	// cache with capacity 2
	store := NewInMemoryCacheStore(2)

	// Enqueue responses for 3 distinct resources + 1 for the cache-miss check.
	for i := 0; i < 4; i++ {
		srv.Enqueue(testutil.MockResponse{
			Status:  http.StatusOK,
			Headers: map[string]string{"Cache-Control": "max-age=3600"},
			Body:    "body",
		})
	}

	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithCache(store))

	// Fill the cache to capacity.
	c.Execute(c.Get(srv.URL() + "/a")) //nolint:errcheck
	c.Execute(c.Get(srv.URL() + "/b")) //nolint:errcheck
	// Adding /c evicts the oldest entry (/a).
	c.Execute(c.Get(srv.URL() + "/c")) //nolint:errcheck

	countAfterFill := srv.RequestCount()
	if countAfterFill != 3 {
		t.Fatalf("expected 3 server requests to fill cache, got %d", countAfterFill)
	}

	// Requesting /a again should be a cache miss (it was evicted).
	c.Execute(c.Get(srv.URL() + "/a")) //nolint:errcheck
	if srv.RequestCount() != 4 {
		t.Errorf("expected 4 server requests (/a was evicted), got %d", srv.RequestCount())
	}
}

// TestInMemoryCacheStore_EvictPrefersExpiredOverOldest whitebox-tests evict's
// two-pass strategy directly: pass 1 must remove every already-expired entry
// before pass 2 falls back to evicting the oldest by insertion order. A live
// entry that happens to be older than an expired one must survive.
func TestInMemoryCacheStore_EvictPrefersExpiredOverOldest(t *testing.T) {
	t.Parallel()
	store := NewInMemoryCacheStore(3).(*inMemoryCacheStore)

	// Insertion order: old-live, expired, newer-live. At capacity (3), the
	// naive "oldest by insertion order" strategy alone would evict old-live;
	// evict() must instead remove the expired one and keep both live entries.
	store.Set("old-live", &CachedResponse{Body: []byte("1")})
	store.Set("expired", &CachedResponse{Body: []byte("2"), ExpiresAt: time.Now().Add(-time.Hour)})
	store.Set("newer-live", &CachedResponse{Body: []byte("3")})

	// Triggers evict() since the store is now at capacity (3 >= maxEntries 3).
	store.Set("fresh", &CachedResponse{Body: []byte("4")})

	if _, ok := store.Get("expired"); ok {
		t.Error("expired entry should have been evicted")
	}
	if _, ok := store.Get("old-live"); !ok {
		t.Error("old-live entry should have survived - expired entries are evicted first")
	}
	if _, ok := store.Get("newer-live"); !ok {
		t.Error("newer-live entry should have survived")
	}
	if _, ok := store.Get("fresh"); !ok {
		t.Error("newly inserted entry should be present")
	}
}

// TestInMemoryCacheStore_EvictCompactsInsertOrderAfterExpiry verifies that
// once an expired key is evicted, insertOrder no longer references it - so a
// later eviction round doesn't try to re-process (or double-delete) a
// already-gone key. Verified indirectly: run eviction across enough rounds
// that, if insertOrder still held stale references, the oldest-by-insertion
// fallback (pass 2) would evict the wrong (already-gone) key and leave the
// store over capacity or in an inconsistent state.
func TestInMemoryCacheStore_EvictCompactsInsertOrderAfterExpiry(t *testing.T) {
	t.Parallel()
	store := NewInMemoryCacheStore(2).(*inMemoryCacheStore)

	store.Set("a", &CachedResponse{Body: []byte("1"), ExpiresAt: time.Now().Add(-time.Hour)}) // expired
	store.Set("b", &CachedResponse{Body: []byte("2")})
	store.Set("c", &CachedResponse{Body: []byte("3")}) // triggers evict(): removes "a"

	store.mu.Lock()
	for _, k := range store.insertOrder {
		if k == "a" {
			t.Error("insertOrder still references the evicted expired key \"a\"")
		}
	}
	gotLen := len(store.insertOrder)
	store.mu.Unlock()
	if gotLen != 2 {
		t.Errorf("insertOrder length = %d, want 2 (b, c)", gotLen)
	}

	// One more insertion should now evict "b" (oldest live), not panic or
	// misbehave from a stale reference to "a".
	store.Set("d", &CachedResponse{Body: []byte("4")})
	if _, ok := store.Get("b"); ok {
		t.Error("expected \"b\" (oldest live) to be evicted next")
	}
	if _, ok := store.Get("c"); !ok {
		t.Error("expected \"c\" to survive")
	}
	if _, ok := store.Get("d"); !ok {
		t.Error("expected \"d\" to be present")
	}
}

// TestInMemoryCacheStore_ConcurrentSetStaysWithinCapacity stresses Set/evict
// under concurrent access from many goroutines and asserts the store never
// exceeds its configured capacity - evict() runs under s.mu, so this should
// hold even without a race detector available in this environment.
func TestInMemoryCacheStore_ConcurrentSetStaysWithinCapacity(t *testing.T) {
	t.Parallel()
	const capacity = 16
	store := NewInMemoryCacheStore(capacity).(*inMemoryCacheStore)

	const goroutines = 32
	const perGoroutine = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				key := fmt.Sprintf("g%d-k%d", g, i)
				store.Set(key, &CachedResponse{Body: []byte("x")})
			}
		}()
	}
	wg.Wait()

	store.mu.Lock()
	entryCount := len(store.entries)
	orderCount := len(store.insertOrder)
	store.mu.Unlock()

	if entryCount > capacity {
		t.Errorf("entries = %d, exceeds capacity %d", entryCount, capacity)
	}
	if orderCount != entryCount {
		t.Errorf("insertOrder length = %d, want equal to entries count %d (no stale references)", orderCount, entryCount)
	}
}

// TestNewInMemoryCacheStore_DefaultsCapacity covers the maxEntries<=0 branch.
func TestNewInMemoryCacheStore_DefaultsCapacity(t *testing.T) {
	t.Parallel()
	for _, n := range []int{0, -1, -100} {
		store := NewInMemoryCacheStore(n).(*inMemoryCacheStore)
		if store.maxEntries != 256 {
			t.Errorf("NewInMemoryCacheStore(%d).maxEntries = %d, want 256", n, store.maxEntries)
		}
	}
}

func TestInMemoryCacheStore_GetSetDelete(t *testing.T) {
	t.Parallel()
	store := NewInMemoryCacheStore(10)

	entry := &CachedResponse{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       []byte("hello"),
	}

	store.Set("key1", entry)

	got, ok := store.Get("key1")
	if !ok {
		t.Fatal("expected cache hit for key1")
	}
	if string(got.Body) != "hello" {
		t.Errorf("expected body 'hello', got %q", string(got.Body))
	}

	store.Delete("key1")
	_, ok = store.Get("key1")
	if ok {
		t.Error("expected cache miss after Delete")
	}
}

// TestInMemoryCacheStore_DeleteThenReSetDoesNotCorruptEvictionOrder guards
// against Delete leaving a stale reference in insertOrder. Without removing
// it, re-Set-ing the same key later looked like a brand-new key (a second,
// duplicate reference got appended), so the next eviction could pop the
// stale first reference and delete the just-re-inserted live entry instead
// of the genuinely oldest one.
func TestInMemoryCacheStore_DeleteThenReSetDoesNotCorruptEvictionOrder(t *testing.T) {
	t.Parallel()
	store := NewInMemoryCacheStore(2)

	entry := func(body string) *CachedResponse {
		return &CachedResponse{StatusCode: http.StatusOK, Status: "200 OK", Body: []byte(body)}
	}

	store.Set("A", entry("a1")) // insertOrder: [A]
	store.Set("B", entry("b1")) // insertOrder: [A, B]
	store.Delete("A")           // insertOrder must become: [B]
	store.Set("A", entry("a2")) // re-add: insertOrder must become: [B, A], not [B, A, A]
	store.Set("C", entry("c1")) // at capacity (2) - must evict the oldest, B, not the freshly re-added A

	if _, ok := store.Get("B"); ok {
		t.Error("expected B (the genuinely oldest entry) to have been evicted")
	}
	got, ok := store.Get("A")
	if !ok {
		t.Fatal("expected A (re-added after B) to still be present")
	}
	if string(got.Body) != "a2" {
		t.Errorf("A body = %q, want %q (the re-added value, not evicted)", got.Body, "a2")
	}
	if _, ok := store.Get("C"); !ok {
		t.Error("expected C to be present")
	}
}

func TestInMemoryCacheStore_Clear(t *testing.T) {
	t.Parallel()
	store := NewInMemoryCacheStore(10)
	store.Set("a", &CachedResponse{StatusCode: 200})
	store.Set("b", &CachedResponse{StatusCode: 200})

	store.Clear()

	if _, ok := store.Get("a"); ok {
		t.Error("expected cache miss for 'a' after Clear")
	}
	if _, ok := store.Get("b"); ok {
		t.Error("expected cache miss for 'b' after Clear")
	}
}

func TestInMemoryCacheStore_ExpiredEntryNotReturned(t *testing.T) {
	t.Parallel()
	store := NewInMemoryCacheStore(10)

	entry := &CachedResponse{
		StatusCode: http.StatusOK,
		ExpiresAt:  time.Now().Add(-time.Second), // already expired
	}
	store.Set("expired", entry)

	_, ok := store.Get("expired")
	if ok {
		t.Error("expired entry should not be returned")
	}
}
