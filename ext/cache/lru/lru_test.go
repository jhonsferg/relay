package lru_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
	relaylru "github.com/jhonsferg/relay/ext/cache/lru"
)

func entry(statusCode int, expiresIn time.Duration) *relay.CachedResponse {
	e := &relay.CachedResponse{
		StatusCode: statusCode,
		Status:     fmt.Sprintf("%d OK", statusCode),
		Headers:    http.Header{"X-Test": []string{"1"}},
		Body:       []byte("body"),
	}
	if expiresIn > 0 {
		e.ExpiresAt = time.Now().Add(expiresIn)
	}
	return e
}

func TestSetAndGet(t *testing.T) {
	c := relaylru.NewLRUCacheStore(10)
	c.Set("key1", entry(200, 0))

	got, ok := c.Get("key1")
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}
	if got.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", got.StatusCode)
	}
}

func TestMiss(t *testing.T) {
	c := relaylru.NewLRUCacheStore(10)
	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("expected miss, got hit")
	}
}

func TestLRUEviction(t *testing.T) {
	c := relaylru.NewLRUCacheStore(3)
	c.Set("a", entry(200, 0))
	c.Set("b", entry(200, 0))
	c.Set("c", entry(200, 0))

	// Access "a" to make it recently used; "b" becomes LRU.
	c.Get("a")
	c.Get("c")

	// Adding "d" should evict "b" (least recently used).
	c.Set("d", entry(200, 0))

	if _, ok := c.Get("b"); ok {
		t.Error("expected 'b' to be evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Error("expected 'a' to still be present")
	}
	if _, ok := c.Get("c"); !ok {
		t.Error("expected 'c' to still be present")
	}
	if _, ok := c.Get("d"); !ok {
		t.Error("expected 'd' to be present")
	}
}

func TestLRUEviction_InsertionOrder(t *testing.T) {
	// Without any access, the first inserted entry is evicted first (FIFO == LRU
	// when nothing has been accessed).
	c := relaylru.NewLRUCacheStore(3)
	c.Set("first", entry(200, 0))
	c.Set("second", entry(200, 0))
	c.Set("third", entry(200, 0))
	c.Set("fourth", entry(200, 0)) // should evict "first"

	if _, ok := c.Get("first"); ok {
		t.Error("expected 'first' to be evicted")
	}
	if _, ok := c.Get("fourth"); !ok {
		t.Error("expected 'fourth' to be present")
	}
}

func TestUpdateExisting(t *testing.T) {
	c := relaylru.NewLRUCacheStore(5)
	c.Set("key", entry(200, 0))
	c.Set("key", entry(404, 0)) // overwrite

	got, ok := c.Get("key")
	if !ok {
		t.Fatal("expected hit")
	}
	if got.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", got.StatusCode)
	}
	if c.Len() != 1 {
		t.Errorf("Len = %d, want 1", c.Len())
	}
}

func TestDelete(t *testing.T) {
	c := relaylru.NewLRUCacheStore(5)
	c.Set("key", entry(200, 0))
	c.Delete("key")

	if _, ok := c.Get("key"); ok {
		t.Error("expected miss after delete")
	}
	if c.Len() != 0 {
		t.Errorf("Len = %d, want 0", c.Len())
	}
}

func TestDelete_NoOp(t *testing.T) {
	c := relaylru.NewLRUCacheStore(5)
	c.Delete("nonexistent") // must not panic
}

func TestClear(t *testing.T) {
	c := relaylru.NewLRUCacheStore(5)
	for i := 0; i < 5; i++ {
		c.Set(fmt.Sprintf("k%d", i), entry(200, 0))
	}
	c.Clear()
	if c.Len() != 0 {
		t.Errorf("Len = %d after Clear, want 0", c.Len())
	}
	if _, ok := c.Get("k0"); ok {
		t.Error("expected miss after Clear")
	}
}

func TestTTLExpiry(t *testing.T) {
	c := relaylru.NewLRUCacheStore(5)
	c.Set("exp", entry(200, 10*time.Millisecond))

	// Immediately → hit.
	if _, ok := c.Get("exp"); !ok {
		t.Error("expected hit before expiry")
	}

	time.Sleep(20 * time.Millisecond)

	// After TTL → miss and evicted.
	if _, ok := c.Get("exp"); ok {
		t.Error("expected miss after expiry")
	}
	if c.Len() != 0 {
		t.Errorf("Len = %d after expiry eviction, want 0", c.Len())
	}
}

func TestNoTTLPersists(t *testing.T) {
	c := relaylru.NewLRUCacheStore(5)
	c.Set("persist", entry(200, 0)) // no TTL
	time.Sleep(10 * time.Millisecond)
	if _, ok := c.Get("persist"); !ok {
		t.Error("expected hit for zero-TTL entry")
	}
}

func TestDefaultCapacity(t *testing.T) {
	c := relaylru.NewLRUCacheStore(0) // 0 → default (256)
	if c.Capacity() != 256 {
		t.Errorf("Capacity = %d, want 256", c.Capacity())
	}
}

func TestConcurrency(t *testing.T) {
	c := relaylru.NewLRUCacheStore(50)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("k%d", n%20)
			c.Set(key, entry(200, 0))
			c.Get(key)
			if n%10 == 0 {
				c.Delete(key)
			}
		}(i)
	}
	wg.Wait()
}

// Verify compile-time interface satisfaction.
var _ relay.CacheStore = (*relaylru.LRUCacheStore)(nil)
