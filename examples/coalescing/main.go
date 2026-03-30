// Package main demonstrates relay's request coalescing (deduplication) feature.
// When many goroutines concurrently request the same URL, WithRequestCoalescing
// ensures that only one real HTTP call is made — all callers share the result.
// This eliminates redundant load on upstream services during traffic spikes,
// cache stampedes, and fan-out scenarios.
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"time"

	relay "github.com/jhonsferg/relay"
)

func main() {
	// -------------------------------------------------------------------------
	// 1. Upstream server that counts actual requests received.
	//
	// With coalescing enabled, many concurrent relay calls for the same URL
	// collapse into a single upstream hit.
	// -------------------------------------------------------------------------
	var serverHits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHits.Add(1)
		// Simulate a slow upstream (e.g. database query, remote API call).
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"hits":%d,"path":%q}`, serverHits.Load(), r.URL.Path)
	}))
	defer srv.Close()

	// -------------------------------------------------------------------------
	// 2. Without coalescing — each goroutine hits the server independently.
	// -------------------------------------------------------------------------
	fmt.Println("=== Without coalescing (10 concurrent requests) ===")
	serverHits.Store(0)

	plainClient := relay.New(relay.WithBaseURL(srv.URL), relay.WithDisableRetry())

	var wg sync.WaitGroup
	results := make([]string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, err := plainClient.Execute(plainClient.Get("/config"))
			if err != nil {
				log.Printf("request %d failed: %v", idx, err)
				return
			}
			results[idx] = resp.String()
		}(i)
	}
	wg.Wait()

	fmt.Printf("  server hits: %d (expected 10 — every goroutine sent its own request)\n\n",
		serverHits.Load())

	// -------------------------------------------------------------------------
	// 3. With coalescing — 10 concurrent requests → 1 actual upstream call.
	// -------------------------------------------------------------------------
	fmt.Println("=== With coalescing (10 concurrent requests) ===")
	serverHits.Store(0)

	coalClient := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithRequestCoalescing(),
		relay.WithDisableRetry(),
	)

	coalResults := make([]string, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, err := coalClient.Execute(coalClient.Get("/config"))
			if err != nil {
				log.Printf("request %d failed: %v", idx, err)
				return
			}
			coalResults[idx] = resp.String()
		}(i)
	}
	wg.Wait()

	fmt.Printf("  server hits: %d (all 10 goroutines shared a single upstream call)\n",
		serverHits.Load())

	// Verify all goroutines received the same body.
	allSame := true
	for _, r := range coalResults[1:] {
		if r != coalResults[0] {
			allSame = false
			break
		}
	}
	fmt.Printf("  all responses identical: %v\n", allSame)
	fmt.Printf("  shared body: %s\n\n", coalResults[0])

	// -------------------------------------------------------------------------
	// 4. Different URLs are NOT coalesced — each is a distinct request.
	// -------------------------------------------------------------------------
	fmt.Println("=== Different URLs are never coalesced ===")
	serverHits.Store(0)

	paths := []string{"/users", "/orders", "/products"}
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			coalClient.Execute(coalClient.Get(path)) //nolint:errcheck
		}(paths[i])
	}
	wg.Wait()
	fmt.Printf("  server hits: %d (3 different paths → 3 independent calls)\n\n",
		serverHits.Load())

	// -------------------------------------------------------------------------
	// 5. POST requests are never coalesced — only GET and HEAD are idempotent.
	// -------------------------------------------------------------------------
	fmt.Println("=== POST requests are never coalesced ===")
	serverHits.Store(0)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			coalClient.Execute( //nolint:errcheck
				coalClient.Post("/submit").WithBody([]byte(`{"action":"create"}`)),
			)
		}()
	}
	wg.Wait()
	fmt.Printf("  server hits: %d (POST is not idempotent — every call goes through)\n\n",
		serverHits.Load())

	// -------------------------------------------------------------------------
	// 6. Authorization header forms part of the coalesce key.
	//
	// Requests with different Authorization values are NOT coalesced even for
	// the same URL — this prevents one user from receiving another's data.
	// -------------------------------------------------------------------------
	fmt.Println("=== Different Authorization headers are never coalesced ===")
	serverHits.Store(0)

	tokens := []string{"token-alice", "token-bob", "token-carol"}
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(tok string) {
			defer wg.Done()
			coalClient.Execute( //nolint:errcheck
				coalClient.Get("/profile").WithBearerToken(tok),
			)
		}(tokens[i])
	}
	wg.Wait()
	fmt.Printf("  server hits: %d (each user's token produces a distinct key)\n\n",
		serverHits.Load())

	// -------------------------------------------------------------------------
	// 7. Coalescing + caching — a powerful combination.
	//
	// Coalescing collapses the thundering herd at the instant of a cache miss;
	// caching prevents repeated upstream calls on subsequent requests.
	// -------------------------------------------------------------------------
	fmt.Println("=== Coalescing + in-memory cache ===")
	serverHits.Store(0)

	cachedClient := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithRequestCoalescing(),
		relay.WithInMemoryCache(128),
		relay.WithDisableRetry(),
	)

	// Wave 1: 10 concurrent requests — all coalesce into 1 upstream call,
	// and the result is written to the cache.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cachedClient.Execute(cachedClient.Get("/config")) //nolint:errcheck
		}()
	}
	wg.Wait()
	wave1 := serverHits.Load()

	// Wave 2: 10 more concurrent requests — served entirely from cache.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cachedClient.Execute(cachedClient.Get("/config")) //nolint:errcheck
		}()
	}
	wg.Wait()
	wave2 := serverHits.Load()

	fmt.Printf("  wave 1 upstream hits: %d (coalesced 10→1)\n", wave1)
	fmt.Printf("  wave 2 upstream hits: %d (served from cache, no coalesce needed)\n", wave2-wave1)
}
