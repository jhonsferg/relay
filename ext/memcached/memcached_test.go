package memcached_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/bradfitz/gomemcache/memcache"

	"github.com/jhonsferg/relay"
	relaymemcached "github.com/jhonsferg/relay/ext/memcached"
)

// ---------------------------------------------------------------------------
// fakeClient - in-memory Client implementation for tests.
//
// Avoids a real memcached daemon while exercising all CacheStore methods.
// ---------------------------------------------------------------------------

type fakeItem struct {
	value      []byte
	expiration int32 // seconds from creation; 0 = never expires
	createdAt  time.Time
}

func (f *fakeItem) expired() bool {
	if f.expiration == 0 {
		return false
	}
	return time.Since(f.createdAt) > time.Duration(f.expiration)*time.Second
}

type fakeClient struct {
	mu    sync.Mutex
	items map[string]*fakeItem
}

func newFakeClient() *fakeClient {
	return &fakeClient{items: make(map[string]*fakeItem)}
}

func (c *fakeClient) Get(key string) (*memcache.Item, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[key]
	if !ok || it.expired() {
		delete(c.items, key)
		return nil, memcache.ErrCacheMiss
	}
	return &memcache.Item{Key: key, Value: it.value, Expiration: it.expiration}, nil
}

func (c *fakeClient) Set(item *memcache.Item) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[item.Key] = &fakeItem{
		value:      item.Value,
		expiration: item.Expiration,
		createdAt:  time.Now(),
	}
	return nil
}

func (c *fakeClient) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[key]; !ok {
		return memcache.ErrCacheMiss
	}
	delete(c.items, key)
	return nil
}

func (c *fakeClient) FlushAll() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*fakeItem)
	return nil
}

// forceExpire sets the creation time of key to the past so it appears expired.
func (c *fakeClient) forceExpire(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if it, ok := c.items[key]; ok {
		it.createdAt = time.Now().Add(-time.Hour)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newStore(t *testing.T) (*relaymemcached.CacheStore, *fakeClient) {
	t.Helper()
	fc := newFakeClient()
	return relaymemcached.NewCacheStore(fc, "relay:test:"), fc
}

func sampleEntry(ttl time.Duration) *relay.CachedResponse {
	var exp time.Time
	if ttl > 0 {
		exp = time.Now().Add(ttl)
	}
	return &relay.CachedResponse{
		StatusCode:   200,
		Status:       "200 OK",
		Headers:      http.Header{"Content-Type": {"application/json"}},
		Body:         []byte(`{"id":1}`),
		ExpiresAt:    exp,
		ETag:         `"v1"`,
		LastModified: "Mon, 01 Jan 2024 00:00:00 GMT",
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestCacheStore_SetAndGet(t *testing.T) {
	t.Parallel()
	store, _ := newStore(t)

	store.Set("key1", sampleEntry(time.Minute))
	got, ok := store.Get("key1")
	if !ok {
		t.Fatal("expected hit")
	}
	if got.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", got.StatusCode)
	}
	if string(got.Body) != `{"id":1}` {
		t.Errorf("Body = %q", string(got.Body))
	}
	if got.ETag != `"v1"` {
		t.Errorf("ETag = %q, want \"v1\"", got.ETag)
	}
	if got.LastModified != "Mon, 01 Jan 2024 00:00:00 GMT" {
		t.Errorf("LastModified = %q", got.LastModified)
	}
}

func TestCacheStore_Miss(t *testing.T) {
	t.Parallel()
	store, _ := newStore(t)

	_, ok := store.Get("missing")
	if ok {
		t.Error("expected miss for absent key")
	}
}

func TestCacheStore_Overwrite(t *testing.T) {
	t.Parallel()
	store, _ := newStore(t)

	e1 := sampleEntry(time.Minute)
	e1.Body = []byte(`{"v":1}`)
	store.Set("k", e1)

	e2 := sampleEntry(time.Minute)
	e2.Body = []byte(`{"v":2}`)
	store.Set("k", e2)

	got, ok := store.Get("k")
	if !ok {
		t.Fatal("expected hit after overwrite")
	}
	if string(got.Body) != `{"v":2}` {
		t.Errorf("Body = %q, want {\"v\":2}", string(got.Body))
	}
}

func TestCacheStore_Delete(t *testing.T) {
	t.Parallel()
	store, _ := newStore(t)

	store.Set("del", sampleEntry(time.Minute))
	if _, ok := store.Get("del"); !ok {
		t.Fatal("expected hit before delete")
	}
	store.Delete("del")
	if _, ok := store.Get("del"); ok {
		t.Error("expected miss after delete")
	}
}

func TestCacheStore_DeleteMissingKeyNoOp(t *testing.T) {
	t.Parallel()
	store, _ := newStore(t)
	// Must not panic.
	store.Delete("nonexistent")
}

func TestCacheStore_Clear(t *testing.T) {
	t.Parallel()
	store, _ := newStore(t)

	store.Set("a", sampleEntry(time.Minute))
	store.Set("b", sampleEntry(time.Minute))
	store.Clear()

	for _, k := range []string{"a", "b"} {
		if _, ok := store.Get(k); ok {
			t.Errorf("expected miss for %q after Clear", k)
		}
	}
}

func TestCacheStore_TTLExpiry(t *testing.T) {
	t.Parallel()
	store, fc := newStore(t)

	store.Set("exp", sampleEntry(500*time.Millisecond))

	if _, ok := store.Get("exp"); !ok {
		t.Fatal("expected hit before expiry")
	}

	// Simulate TTL elapsing by backdating the fake item's creation time.
	// The fake client encodes the key; find it by brute-force.
	fc.mu.Lock()
	for k := range fc.items {
		fc.items[k].createdAt = time.Now().Add(-time.Hour)
	}
	fc.mu.Unlock()

	if _, ok := store.Get("exp"); ok {
		t.Error("expected miss after TTL expiry")
	}
}

func TestCacheStore_NoTTLPersists(t *testing.T) {
	t.Parallel()
	store, _ := newStore(t)

	// Zero ExpiresAt = no TTL.
	store.Set("persistent", sampleEntry(0))

	got, ok := store.Get("persistent")
	if !ok {
		t.Fatal("expected hit for persistent entry")
	}
	if got.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", got.StatusCode)
	}
}

func TestCacheStore_AlreadyExpiredNotStored(t *testing.T) {
	t.Parallel()
	store, _ := newStore(t)

	e := sampleEntry(0)
	e.ExpiresAt = time.Now().Add(-time.Second) // already past
	store.Set("past", e)

	if _, ok := store.Get("past"); ok {
		t.Error("expected miss for already-expired entry")
	}
}

func TestCacheStore_HeadersRoundTrip(t *testing.T) {
	t.Parallel()
	store, _ := newStore(t)

	entry := &relay.CachedResponse{
		StatusCode: 200,
		Status:     "200 OK",
		Headers: http.Header{
			"Content-Type":  {"application/json"},
			"Cache-Control": {"max-age=300"},
			"X-Multi":       {"v1", "v2"},
		},
		Body: []byte(`{}`),
	}
	store.Set("hdr", entry)

	got, ok := store.Get("hdr")
	if !ok {
		t.Fatal("expected hit")
	}
	if got.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", got.Headers.Get("Content-Type"))
	}
	if vs := got.Headers["X-Multi"]; len(vs) != 2 {
		t.Errorf("X-Multi len = %d, want 2", len(vs))
	}
}

func TestCacheStore_KeyEncodingHandlesSpecialChars(t *testing.T) {
	t.Parallel()
	store, _ := newStore(t)

	// Cache keys contain colons and slashes which are invalid in memcached.
	key := "GET:https://api.example.com/v1/users?page=1&limit=10"
	store.Set(key, sampleEntry(time.Minute))

	got, ok := store.Get(key)
	if !ok {
		t.Fatal("expected hit for key with special characters")
	}
	if got.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", got.StatusCode)
	}
}

func TestCacheStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	store, _ := newStore(t)

	const goroutines = 20
	done := make(chan struct{}, goroutines)
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			key := "concurrent-key"
			store.Set(key, sampleEntry(time.Minute))
			store.Get(key) //nolint:errcheck
			store.Delete(key)
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
}

func TestCacheStore_IntegrationWithRelayClient(t *testing.T) {
	t.Parallel()

	store, _ := newStore(t)

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Cache-Control", "max-age=60")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"hits":%d}`, hits)
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithCache(store),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	// First request: miss → server.
	resp1, err := c.Execute(c.Get("/resource"))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if hits != 1 {
		t.Errorf("hits after first request = %d, want 1", hits)
	}

	// Second request: hit → cache.
	resp2, err := c.Execute(c.Get("/resource"))
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if hits != 1 {
		t.Errorf("hits after cached request = %d, want 1", hits)
	}
	if resp1.String() != resp2.String() {
		t.Errorf("cached body mismatch: %q vs %q", resp1.String(), resp2.String())
	}
}

// ---------------------------------------------------------------------------
// Verify that fakeClient satisfies the Client interface.
// ---------------------------------------------------------------------------

var _ relaymemcached.Client = (*fakeClient)(nil)

// ensureConcreteClientSatisfiesInterface is a compile-time check that
// *memcache.Client satisfies the exported Client interface.
func TestConcreteClientSatisfiesInterface(t *testing.T) {
	var _ relaymemcached.Client = (*memcache.Client)(nil)
	// If this compiles, the assertion holds.
}

// errClient is a Client that always returns errors.
type errClient struct{}

func (e *errClient) Get(_ string) (*memcache.Item, error) { return nil, errors.New("get error") }
func (e *errClient) Set(_ *memcache.Item) error           { return errors.New("set error") }
func (e *errClient) Delete(_ string) error                { return errors.New("delete error") }
func (e *errClient) FlushAll() error                      { return errors.New("flush error") }

func TestCacheStore_ClientErrorsAreHandledGracefully(t *testing.T) {
	t.Parallel()

	store := relaymemcached.NewCacheStore(&errClient{}, "prefix:")

	// None of these should panic.
	_, _ = store.Get("k")
	store.Set("k", sampleEntry(time.Minute))
	store.Delete("k")
	store.Clear()
}
