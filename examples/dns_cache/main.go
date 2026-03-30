// Package main demonstrates relay's WithDNSCache option, which caches DNS
// resolution results for a configurable TTL. This reduces latency on
// keep-alive-heavy workloads and prevents thundering-herd re-resolution when
// many goroutines dial the same host at the same time.
package main

import (
	"context"
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
	// 1. Basic usage - 30-second DNS TTL.
	// -------------------------------------------------------------------------
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDNSCache(30*time.Second),
	)

	fmt.Println("=== Basic DNS cache (30 s TTL) ===")
	for i := 1; i <= 3; i++ {
		resp, err := client.Execute(client.Get("/api"))
		if err != nil {
			log.Fatalf("request %d failed: %v", i, err)
		}
		fmt.Printf("  request %d: status=%d body=%s\n", i, resp.StatusCode, resp.String())
	}

	// -------------------------------------------------------------------------
	// 2. Concurrency - many goroutines dial simultaneously.
	//    Without caching each goroutine would trigger its own OS DNS lookup.
	//    With the cache only the first lookup goes to the resolver; the rest
	//    read from the in-memory cache.
	// -------------------------------------------------------------------------
	fmt.Println("\n=== 20 concurrent requests sharing DNS cache ===")

	var (
		wg      sync.WaitGroup
		success atomic.Int32
	)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Execute(client.Get("/api"))
			if err == nil && resp.IsSuccess() {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	fmt.Printf("  %d/20 requests succeeded\n", success.Load())

	// -------------------------------------------------------------------------
	// 3. Short TTL - cache expires and triggers fresh lookups.
	// -------------------------------------------------------------------------
	fmt.Println("\n=== Short TTL (10 ms) ===")
	shortClient := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDNSCache(10*time.Millisecond),
	)

	for i := 1; i <= 3; i++ {
		time.Sleep(15 * time.Millisecond) // let TTL expire between requests
		resp, err := shortClient.Execute(shortClient.Get("/api"))
		if err != nil {
			log.Fatalf("short TTL request %d failed: %v", i, err)
		}
		fmt.Printf("  request %d: status=%d (cache expired, re-resolved)\n", i, resp.StatusCode)
	}

	// -------------------------------------------------------------------------
	// 4. Comparison with WithDNSOverride.
	//
	// WithDNSOverride pins a specific IP for a hostname (bypasses DNS entirely).
	// WithDNSCache caches the result of normal DNS resolution.
	// They are mutually exclusive in transport wiring - use one or the other.
	// -------------------------------------------------------------------------
	fmt.Println("\n=== DNS cache vs DNS override ===")

	// Simulate a multi-backend round-robin by counting requests.
	var reqCount atomic.Int32
	multiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := reqCount.Add(1)
		fmt.Fprintf(w, `{"request":%d}`, n)
	}))
	defer multiSrv.Close()

	cachedClient := relay.New(
		relay.WithBaseURL(multiSrv.URL),
		relay.WithDNSCache(60*time.Second), // cache for 60 s
	)

	fmt.Println("  cached client (60 s TTL):")
	for i := 1; i <= 3; i++ {
		resp, err := cachedClient.Execute(cachedClient.Get("/"))
		if err != nil {
			log.Fatalf("request %d: %v", i, err)
		}
		fmt.Printf("    request %d → %s\n", i, resp.String())
	}

	// -------------------------------------------------------------------------
	// 5. Graceful shutdown.
	// -------------------------------------------------------------------------
	fmt.Println("\n=== Shutdown ===")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	for _, c := range []*relay.Client{client, shortClient, cachedClient} {
		if err := c.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}
	fmt.Println("  all clients shut down.")
}
