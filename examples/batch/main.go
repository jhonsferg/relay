// Package main demonstrates relay's ExecuteBatch: sending many requests
// concurrently with a controlled parallelism limit, using WithTag for
// observability labels, and handling per-item results and errors.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	relay "github.com/jhonsferg/relay"
)

func main() {
	// ---------------------------------------------------------------------------
	// Test server: echoes the requested path and occasionally returns 500.
	// ---------------------------------------------------------------------------
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		// Simulate an intermittent server error every 7th request.
		if n%7 == 0 {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, `{"error":"simulated failure","seq":%d}`, n)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"path":%q,"seq":%d}`, r.URL.Path, n)
	}))
	defer srv.Close()

	// ---------------------------------------------------------------------------
	// Build a client with a short per-request timeout and retries disabled so
	// every result maps 1-to-1 with a single server attempt.
	// ---------------------------------------------------------------------------
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithTimeout(10*time.Second),
		relay.WithDisableRetry(),
	)

	// ---------------------------------------------------------------------------
	// Build a batch of 20 requests.
	//
	// WithTag attaches client-side labels that are never sent as HTTP headers.
	// They are visible inside OnBeforeRequest / OnAfterResponse hooks, making
	// them useful for logging, metrics dimensions, and routing decisions.
	// ---------------------------------------------------------------------------
	const batchSize = 20
	requests := make([]*relay.Request, batchSize)
	for i := 0; i < batchSize; i++ {
		userID := i + 1
		requests[i] = client.Get(fmt.Sprintf("/users/%d", userID)).
			WithTag("operation", "FetchUser").
			WithTag("user_id", fmt.Sprintf("%d", userID)).
			WithTag("batch", "user-prefetch")
	}

	// ---------------------------------------------------------------------------
	// ExecuteBatch dispatches all requests concurrently, throttled to at most
	// maxConcurrency in-flight at once. Results preserve input order regardless
	// of completion order.
	// ---------------------------------------------------------------------------
	const maxConcurrency = 5
	ctx := context.Background()

	fmt.Printf("Sending batch of %d requests (concurrency=%d)…\n\n", batchSize, maxConcurrency)
	start := time.Now()
	results := client.ExecuteBatch(ctx, requests, maxConcurrency)
	elapsed := time.Since(start)

	// ---------------------------------------------------------------------------
	// Process results.
	//
	// result.Index always matches the original position in the requests slice.
	// result.Err is non-nil for transport errors; check resp.IsError() for
	// HTTP-level errors (4xx/5xx).
	// ---------------------------------------------------------------------------
	var (
		successCount int
		errorCount   int
		httpErrCount int
	)

	for _, result := range results {
		// Retrieve the tag we set on the original request for correlation.
		originalTag := requests[result.Index].Tag("operation")

		switch {
		case result.Err != nil:
			// Transport error: network failure, timeout, circuit breaker open, etc.
			fmt.Printf("  [%02d] %-12s TRANSPORT ERROR: %v\n",
				result.Index, originalTag, result.Err)
			errorCount++

		case result.Response.IsError():
			// HTTP error: 4xx or 5xx from the server.
			fmt.Printf("  [%02d] %-12s HTTP %d: %s\n",
				result.Index, originalTag, result.Response.StatusCode, result.Response.String())
			httpErrCount++

		default:
			// Success: 2xx response.
			fmt.Printf("  [%02d] %-12s OK %d: %s\n",
				result.Index, originalTag, result.Response.StatusCode, result.Response.String())
			successCount++
		}
	}

	fmt.Printf("\nBatch summary - elapsed: %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  success      : %d\n", successCount)
	fmt.Printf("  HTTP errors  : %d\n", httpErrCount)
	fmt.Printf("  transport err: %d\n", errorCount)
	fmt.Printf("  total        : %d\n", len(results))

	// ---------------------------------------------------------------------------
	// Batch with context cancellation
	//
	// Pass a cancelable context to abort remaining requests when the caller
	// no longer needs the results (e.g., the parent request was canceled).
	// ---------------------------------------------------------------------------
	fmt.Println("\n=== Batch with context cancellation ===")

	cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer slowSrv.Close()

	slowClient := relay.New(relay.WithBaseURL(slowSrv.URL), relay.WithDisableRetry())
	slowReqs := make([]*relay.Request, 10)
	for i := range slowReqs {
		slowReqs[i] = slowClient.Get("/slow")
	}

	canceledResults := slowClient.ExecuteBatch(cancelCtx, slowReqs, 2)

	var canceled, completed int
	for _, r := range canceledResults {
		if r.Err != nil {
			canceled++
		} else {
			completed++
		}
	}
	fmt.Printf("  completed: %d, canceled/error: %d\n", completed, canceled)

	if canceled == 0 {
		log.Println("  note: all requests completed before context expired")
	}
}
