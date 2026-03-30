package redis_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redisclient "github.com/redis/go-redis/v9"

	"github.com/jhonsferg/relay"
	relayredis "github.com/jhonsferg/relay/ext/redis"
)

// newTestStore starts an in-process miniredis server and returns a CacheStore
// wired to it. The server is closed when the test finishes.
func newTestStore(t *testing.T) (*relayredis.CacheStore, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redisclient.NewClient(&redisclient.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	return relayredis.NewCacheStore(rdb, "relay:test:"), mr
}

// sampleEntry builds a minimal CachedResponse. ttl <= 0 means no expiry.
func sampleEntry(ttl time.Duration) *relay.CachedResponse {
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}
	return &relay.CachedResponse{
		StatusCode:   200,
		Status:       "200 OK",
		Headers:      http.Header{"Content-Type": {"application/json"}},
		Body:         []byte(`{"id":1}`),
		ExpiresAt:    expiresAt,
		ETag:         `"abc123"`,
		LastModified: "Wed, 21 Oct 2015 07:28:00 GMT",
	}
}

func TestCacheStore_SetAndGet(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)

	store.Set("key1", sampleEntry(time.Minute))

	got, ok := store.Get("key1")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if got.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", got.StatusCode)
	}
	if string(got.Body) != `{"id":1}` {
		t.Errorf("Body = %q, want {\"id\":1}", string(got.Body))
	}
	if got.ETag != `"abc123"` {
		t.Errorf("ETag = %q, want \"abc123\"", got.ETag)
	}
	if got.LastModified != "Wed, 21 Oct 2015 07:28:00 GMT" {
		t.Errorf("LastModified = %q", got.LastModified)
	}
}

func TestCacheStore_MissingKey(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)

	_, ok := store.Get("nonexistent")
	if ok {
		t.Error("expected cache miss for nonexistent key")
	}
}

func TestCacheStore_Overwrite(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)

	first := sampleEntry(time.Minute)
	first.Body = []byte(`{"v":1}`)
	store.Set("k", first)

	second := sampleEntry(time.Minute)
	second.Body = []byte(`{"v":2}`)
	store.Set("k", second)

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
	store, _ := newTestStore(t)

	store.Set("del-key", sampleEntry(time.Minute))

	if _, ok := store.Get("del-key"); !ok {
		t.Fatal("expected hit before delete")
	}

	store.Delete("del-key")

	if _, ok := store.Get("del-key"); ok {
		t.Error("expected miss after delete")
	}
}

func TestCacheStore_DeleteMissingKeyNoOp(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)

	// Should not panic or return an error.
	store.Delete("does-not-exist")
}

func TestCacheStore_Clear(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)

	store.Set("c1", sampleEntry(time.Minute))
	store.Set("c2", sampleEntry(time.Minute))
	store.Set("c3", sampleEntry(time.Minute))

	store.Clear()

	for _, k := range []string{"c1", "c2", "c3"} {
		if _, ok := store.Get(k); ok {
			t.Errorf("expected miss for %q after Clear", k)
		}
	}
}

func TestCacheStore_ClearOnlyRemovesPrefixedKeys(t *testing.T) {
	t.Parallel()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()

	rdb := redisclient.NewClient(&redisclient.Options{Addr: mr.Addr()})
	defer rdb.Close()

	// Two stores sharing the same Redis but different prefixes.
	storeA := relayredis.NewCacheStore(rdb, "relay:a:")
	storeB := relayredis.NewCacheStore(rdb, "relay:b:")

	storeA.Set("shared", sampleEntry(time.Minute))
	storeB.Set("shared", sampleEntry(time.Minute))

	storeA.Clear()

	// storeA's key gone.
	if _, ok := storeA.Get("shared"); ok {
		t.Error("storeA: expected miss after Clear")
	}
	// storeB's key unaffected.
	if _, ok := storeB.Get("shared"); !ok {
		t.Error("storeB: expected hit after storeA.Clear")
	}
}

func TestCacheStore_TTLExpiry(t *testing.T) {
	t.Parallel()
	store, mr := newTestStore(t)

	store.Set("exp", sampleEntry(500*time.Millisecond))

	if _, ok := store.Get("exp"); !ok {
		t.Fatal("expected hit before expiry")
	}

	// Fast-forward miniredis clock past the TTL.
	mr.FastForward(time.Second)

	if _, ok := store.Get("exp"); ok {
		t.Error("expected miss after TTL expiry")
	}
}

func TestCacheStore_NoTTLEntryPersists(t *testing.T) {
	t.Parallel()
	store, mr := newTestStore(t)

	// Zero ExpiresAt = no TTL.
	store.Set("persistent", sampleEntry(0))

	mr.FastForward(24 * time.Hour)

	if _, ok := store.Get("persistent"); !ok {
		t.Error("expected persistent entry to survive time fast-forward")
	}
}

func TestCacheStore_AlreadyExpiredEntryNotStored(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)

	// ExpiresAt in the past - Set should be a no-op.
	entry := sampleEntry(0)
	entry.ExpiresAt = time.Now().Add(-time.Second)
	store.Set("past", entry)

	if _, ok := store.Get("past"); ok {
		t.Error("expected miss for already-expired entry")
	}
}

func TestCacheStore_HeadersRoundTrip(t *testing.T) {
	t.Parallel()
	store, _ := newTestStore(t)

	entry := &relay.CachedResponse{
		StatusCode: 200,
		Status:     "200 OK",
		Headers: http.Header{
			"Content-Type":  {"application/json"},
			"Cache-Control": {"max-age=60"},
			"X-Multi":       {"v1", "v2"},
		},
		Body: []byte(`{}`),
	}
	store.Set("headers", entry)

	got, ok := store.Get("headers")
	if !ok {
		t.Fatal("expected hit")
	}
	if got.Headers.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", got.Headers.Get("Content-Type"))
	}
	if vals := got.Headers["X-Multi"]; len(vals) != 2 {
		t.Errorf("X-Multi len = %d, want 2", len(vals))
	}
}

func TestCacheStore_IntegrationWithRelayClient(t *testing.T) {
	t.Parallel()

	store, _ := newTestStore(t)

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Cache-Control", "max-age=60")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithCache(store),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	// First request: cache miss - hits the server.
	resp, err := c.Execute(c.Get("/resource"))
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("first status = %d, want 200", resp.StatusCode)
	}
	if hits != 1 {
		t.Errorf("hits after first request = %d, want 1", hits)
	}

	// Second request: cache hit - server not contacted.
	resp, err = c.Execute(c.Get("/resource"))
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("second status = %d, want 200", resp.StatusCode)
	}
	if hits != 1 {
		t.Errorf("hits after cached request = %d, want 1 (no new server hit)", hits)
	}
	if body := resp.String(); body != `{"ok":true}` {
		t.Errorf("cached body = %q, want {\"ok\":true}", body)
	}
}
