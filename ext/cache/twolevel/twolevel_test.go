package twolevel_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
	relaylru "github.com/jhonsferg/relay/ext/cache/lru"
	relaytwo "github.com/jhonsferg/relay/ext/cache/twolevel"
)

// fakeStore is a simple in-memory CacheStore for testing L2 independently.
type fakeStore struct {
	mu    sync.Mutex
	items map[string]*relay.CachedResponse
	gets  int
	sets  int
}

func newFake() *fakeStore { return &fakeStore{items: make(map[string]*relay.CachedResponse)} }

func (f *fakeStore) Get(key string) (*relay.CachedResponse, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	v, ok := f.items[key]
	if !ok {
		return nil, false
	}
	if !v.ExpiresAt.IsZero() && time.Now().After(v.ExpiresAt) {
		delete(f.items, key)
		return nil, false
	}
	return v, true
}

func (f *fakeStore) Set(key string, entry *relay.CachedResponse) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets++
	f.items[key] = entry
}

func (f *fakeStore) Delete(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.items, key)
}

func (f *fakeStore) Clear() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items = make(map[string]*relay.CachedResponse)
}

func entry(code int) *relay.CachedResponse {
	return &relay.CachedResponse{
		StatusCode: code,
		Status:     fmt.Sprintf("%d OK", code),
		Headers:    http.Header{},
		Body:       []byte("body"),
	}
}

// -- Basic operations ----------------------------------------------------------

func TestSet_WritesToBothLevels(t *testing.T) {
	l1 := newFake()
	l2 := newFake()
	store := relaytwo.New(l1, l2)

	store.Set("key", entry(200))

	if l1.sets != 1 {
		t.Errorf("l1.sets = %d, want 1", l1.sets)
	}
	if l2.sets != 1 {
		t.Errorf("l2.sets = %d, want 1", l2.sets)
	}
}

func TestGet_L1Hit(t *testing.T) {
	l1 := newFake()
	l2 := newFake()
	store := relaytwo.New(l1, l2)

	store.Set("key", entry(200))
	l1.gets = 0 // reset counter

	got, ok := store.Get("key")
	if !ok {
		t.Fatal("expected hit, got miss")
	}
	if got.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", got.StatusCode)
	}
	if l2.gets != 0 {
		t.Errorf("l2 was queried on L1 hit (gets=%d)", l2.gets)
	}
}

func TestGet_L1MissL2Hit_BackfillsL1(t *testing.T) {
	l1 := relaylru.NewLRUCacheStore(10)
	l2 := newFake()
	store := relaytwo.New(l1, l2)

	// Write directly to L2 only.
	l2.Set("key", entry(201))

	// First Get: L1 miss → L2 hit → backfill L1.
	got, ok := store.Get("key")
	if !ok {
		t.Fatal("expected hit from L2")
	}
	if got.StatusCode != 201 {
		t.Errorf("StatusCode = %d, want 201", got.StatusCode)
	}
	if l1.Len() != 1 {
		t.Errorf("L1 not backfilled: Len = %d, want 1", l1.Len())
	}

	// Second Get: now served from L1 without touching L2.
	l2.gets = 0
	store.Get("key")
	if l2.gets != 0 {
		t.Errorf("L2 queried on second Get (gets=%d), expected L1 cache hit", l2.gets)
	}
}

func TestGet_BothMiss(t *testing.T) {
	store := relaytwo.New(newFake(), newFake())
	if _, ok := store.Get("missing"); ok {
		t.Error("expected miss, got hit")
	}
}

func TestDelete_BothLevels(t *testing.T) {
	l1 := newFake()
	l2 := newFake()
	store := relaytwo.New(l1, l2)

	store.Set("key", entry(200))
	store.Delete("key")

	if _, ok := l1.Get("key"); ok {
		t.Error("key still in L1 after Delete")
	}
	if _, ok := l2.Get("key"); ok {
		t.Error("key still in L2 after Delete")
	}
}

func TestClear_BothLevels(t *testing.T) {
	l1 := newFake()
	l2 := newFake()
	store := relaytwo.New(l1, l2)

	store.Set("a", entry(200))
	store.Set("b", entry(200))
	store.Clear()

	if _, ok := l1.Get("a"); ok {
		t.Error("L1 not cleared")
	}
	if _, ok := l2.Get("b"); ok {
		t.Error("L2 not cleared")
	}
}

// -- Integration with relay ----------------------------------------------------

func TestIntegration_TwoLevelWithRelay(t *testing.T) {
	l1 := relaylru.NewLRUCacheStore(16)
	l2 := newFake()
	store := relaytwo.New(l1, l2)

	// Seed L2 with a cached response.
	l2.Set("GET:http://api.example.com/items", &relay.CachedResponse{
		StatusCode: 200,
		Status:     "200 OK",
		Headers:    http.Header{"Cache-Control": []string{"max-age=60"}},
		Body:       []byte(`{"items":[1,2,3]}`),
		ExpiresAt:  time.Now().Add(60 * time.Second),
	})

	// Verify the two-level store serves the response and backfills L1.
	resp, ok := store.Get("GET:http://api.example.com/items")
	if !ok {
		t.Fatal("expected hit from L2")
	}
	if string(resp.Body) != `{"items":[1,2,3]}` {
		t.Errorf("body = %q", resp.Body)
	}
	if l1.Len() != 1 {
		t.Error("L1 was not backfilled")
	}
}

// -- Nil panic -----------------------------------------------------------------

func TestNew_NilL1Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil l1")
		}
	}()
	relaytwo.New(nil, newFake())
}

func TestNew_NilL2Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil l2")
		}
	}()
	relaytwo.New(newFake(), nil)
}

// Compile-time interface check.
var _ relay.CacheStore = (*relaytwo.TwoLevelCacheStore)(nil)
