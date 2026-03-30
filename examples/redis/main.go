// Package main demonstrates relay's Redis-backed HTTP cache via
// github.com/jhonsferg/relay/ext/redis.
//
// The example uses github.com/alicebob/miniredis/v2 as an in-process Redis
// server so it runs without any external dependency. In production, replace
// miniredis with a real *redis.Client:
//
//	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
//	store := relayredis.NewCacheStore(rdb, "myapp:http-cache:")
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/alicebob/miniredis/v2"
	redisclient "github.com/redis/go-redis/v9"

	relay "github.com/jhonsferg/relay"
	relayredis "github.com/jhonsferg/relay/ext/redis"
)

func main() {
	// ---------------------------------------------------------------------------
	// 1. Start an in-process Redis (miniredis).
	//
	// Replace this block with a real redis.NewClient for production use.
	// ---------------------------------------------------------------------------
	mr, err := miniredis.Run()
	if err != nil {
		log.Fatalf("miniredis.Run: %v", err)
	}
	defer mr.Close()

	rdb := redisclient.NewClient(&redisclient.Options{Addr: mr.Addr()})
	defer rdb.Close()

	fmt.Printf("In-process Redis listening at %s\n\n", mr.Addr())

	// ---------------------------------------------------------------------------
	// 2. Build the relay CacheStore backed by Redis.
	//
	// The prefix "demo:http:" namespaces all cache keys - safe to share a Redis
	// instance with other apps. Multiple relay clients can use different prefixes
	// on the same rdb connection.
	// ---------------------------------------------------------------------------
	store := relayredis.NewCacheStore(rdb, "demo:http:")

	// ---------------------------------------------------------------------------
	// 3. Origin server: counts hits and emits Cache-Control: max-age=60 so relay
	// knows responses are cacheable for 60 seconds.
	// ---------------------------------------------------------------------------
	var (
		mu         sync.Mutex
		serverHits int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		serverHits++
		hits := serverHits
		mu.Unlock()

		w.Header().Set("Cache-Control", "max-age=60")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("ETag", fmt.Sprintf(`"v%d"`, hits))
		fmt.Fprintf(w, `{"hits":%d,"path":%q}`, hits, r.URL.Path)
	}))
	defer srv.Close()

	// ---------------------------------------------------------------------------
	// 4. Build the relay client wired to the Redis cache store.
	// ---------------------------------------------------------------------------
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithCache(store),
		relay.WithDisableRetry(),
		relay.WithTimeout(5*time.Second),
	)

	printHits := func(label string, serverCalls int) {
		fmt.Printf("  %-42s server hits so far: %d\n", label, serverCalls)
	}

	// ---------------------------------------------------------------------------
	// 5. Cache MISS - first request goes to the origin.
	// ---------------------------------------------------------------------------
	fmt.Println("=== Request 1: cache MISS ===")
	resp, err := client.Execute(client.Get("/products/1"))
	if err != nil {
		log.Fatalf("request 1: %v", err)
	}
	fmt.Printf("  Status: %d   Body: %s\n", resp.StatusCode, resp.String())
	printHits("(expected 1 - first request)", serverHits)

	// ---------------------------------------------------------------------------
	// 6. Cache HIT - same URL served from Redis, origin not contacted.
	// ---------------------------------------------------------------------------
	fmt.Println("\n=== Request 2: cache HIT (same URL) ===")
	resp, err = client.Execute(client.Get("/products/1"))
	if err != nil {
		log.Fatalf("request 2: %v", err)
	}
	fmt.Printf("  Status: %d   Body: %s\n", resp.StatusCode, resp.String())
	printHits("(expected 1 - served from Redis cache)", serverHits)

	// ---------------------------------------------------------------------------
	// 7. Different URL - new cache MISS.
	// ---------------------------------------------------------------------------
	fmt.Println("\n=== Request 3: cache MISS (different path) ===")
	resp, err = client.Execute(client.Get("/products/2"))
	if err != nil {
		log.Fatalf("request 3: %v", err)
	}
	fmt.Printf("  Status: %d   Body: %s\n", resp.StatusCode, resp.String())
	printHits("(expected 2 - new path)", serverHits)

	// ---------------------------------------------------------------------------
	// 8. Cache-Control: no-cache forces revalidation even if cached.
	// ---------------------------------------------------------------------------
	fmt.Println("\n=== Request 4: forced revalidation (Cache-Control: no-cache) ===")
	resp, err = client.Execute(
		client.Get("/products/1").WithHeader("Cache-Control", "no-cache"),
	)
	if err != nil {
		log.Fatalf("request 4: %v", err)
	}
	fmt.Printf("  Status: %d   Body: %s\n", resp.StatusCode, resp.String())
	printHits("(expected 3 - revalidated with origin)", serverHits)

	// ---------------------------------------------------------------------------
	// 9. TTL expiry: fast-forward miniredis clock past max-age.
	// ---------------------------------------------------------------------------
	fmt.Println("\n=== Request 5: after TTL expiry (cache MISS again) ===")
	mr.FastForward(65 * time.Second)

	resp, err = client.Execute(client.Get("/products/1"))
	if err != nil {
		log.Fatalf("request 5: %v", err)
	}
	fmt.Printf("  Status: %d   Body: %s\n", resp.StatusCode, resp.String())
	printHits("(expected 4 - Redis TTL expired, fetched fresh)", serverHits)

	// ---------------------------------------------------------------------------
	// 10. Manual cache invalidation via store.Delete / store.Clear.
	// ---------------------------------------------------------------------------
	fmt.Println("\n=== Manual cache operations ===")

	// Prime the cache.
	client.Execute(client.Get("/products/10")) //nolint:errcheck
	client.Execute(client.Get("/products/11")) //nolint:errcheck
	fmt.Printf("  Primed 2 entries. Server hits now: %d\n", serverHits)

	// Delete one specific entry (e.g., after a write invalidates the resource).
	store.Delete("GET:" + srv.URL + "/products/10")
	fmt.Println("  Deleted /products/10 from cache.")

	// Clear ALL keys under the "demo:http:" prefix - other Redis data unaffected.
	store.Clear()
	fmt.Println("  Cleared all demo:http: keys.")

	// Next request is a cache miss.
	resp, err = client.Execute(client.Get("/products/10"))
	if err != nil {
		log.Fatalf("post-clear request: %v", err)
	}
	fmt.Printf("  Post-clear: %d   Server hits now: %d\n", resp.StatusCode, serverHits)

	// ---------------------------------------------------------------------------
	// 11. Multiple clients sharing the same Redis store.
	//
	// Two relay clients backed by the same store; one client's cached response
	// is served to the other.
	// ---------------------------------------------------------------------------
	fmt.Println("\n=== Two clients sharing one Redis store ===")
	sharedStore := relayredis.NewCacheStore(rdb, "shared:")

	clientA := relay.New(relay.WithBaseURL(srv.URL), relay.WithCache(sharedStore), relay.WithDisableRetry())
	clientB := relay.New(relay.WithBaseURL(srv.URL), relay.WithCache(sharedStore), relay.WithDisableRetry())

	hitsBefore := serverHits
	clientA.Execute(client.Get("/shared/resource")) //nolint:errcheck
	clientB.Execute(client.Get("/shared/resource")) //nolint:errcheck

	hitsAfter := serverHits
	fmt.Printf("  Client A fetched, Client B served from shared cache.\n")
	fmt.Printf("  Net new server hits: %d (expected 1)\n", hitsAfter-hitsBefore)
}
