// Package common contains core benchmarks for the relay HTTP client,
// measuring fundamental performance characteristics like throughput,
// latency, and the overhead of relay's execution pipeline.
//
// These benchmarks focus on basic Execute() operations with minimal
// features enabled, establishing performance baselines.
package common

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	relay "github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/testutil"
)

// ---------------------------------------------------------------------------
// BenchmarkExecute_Simple
//
// Measures the overhead of a single GET request against a mock server that
// always returns 200 OK instantly. This is the baseline cost of relay's
// pipeline (rate limiter check, circuit breaker, retrier, response wrapping).
// ---------------------------------------------------------------------------
func BenchmarkExecute_Simple(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(5*time.Second),
	)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Enqueue a fresh 200 OK for each iteration.
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusOK,
			Body:   `{"id":1,"name":"relay"}`,
		})

		resp, err := client.Execute(client.Get("/bench"))
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("unexpected status %d", resp.StatusCode)
		}
	}
}

// ---------------------------------------------------------------------------
// BenchmarkExecute_WithRetry
//
// Measures the cost of a request that fails once with 503 and then succeeds.
// This exercises the full retry loop: backoff computation, sleep (skipped in
// tests because the sleep is real - keep MaxAttempts low), and response
// re-parsing.
//
// Note: the benchmark uses a 0-duration initial interval so the retry fires
// immediately, keeping wall time reasonable.
// ---------------------------------------------------------------------------
func BenchmarkExecute_WithRetry(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithRetry(&relay.RetryConfig{
			MaxAttempts:     2,                // 1 failure + 1 success
			InitialInterval: time.Microsecond, // near-zero backoff for benchmarks
			MaxInterval:     time.Microsecond,
			Multiplier:      1.0,
			RandomFactor:    0,
			RetryableStatus: []int{http.StatusServiceUnavailable},
		}),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(5*time.Second),
	)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// First response: 503 → triggers retry.
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusServiceUnavailable,
			Body:   `{"error":"unavailable"}`,
		})
		// Second response: 200 → returned to caller.
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusOK,
			Body:   `{"id":1}`,
		})

		resp, err := client.Execute(client.Get("/bench-retry"))
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("unexpected final status %d", resp.StatusCode)
		}
	}
}

// ---------------------------------------------------------------------------
// BenchmarkExecute_WithCache
//
// Measures the cost when the second request (and beyond) is served entirely
// from the in-memory cache - no network round trip occurs after the first hit.
// ---------------------------------------------------------------------------
func BenchmarkExecute_WithCache(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithInMemoryCache(512),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(5*time.Second),
	)

	// Prime the cache: enqueue a response with Cache-Control max-age so relay
	// stores it, then perform the first (cache-populating) request.
	srv.Enqueue(testutil.MockResponse{
		Status: http.StatusOK,
		Headers: map[string]string{
			"Content-Type":  "application/json",
			"Cache-Control": "max-age=3600",
		},
		Body: `{"cached":true,"value":42}`,
	})

	_, err := client.Execute(client.Get("/cached-resource"))
	if err != nil {
		b.Fatalf("cache prime failed: %v", err)
	}

	// All subsequent requests in the benchmark loop are cache hits -
	// no more server responses need to be enqueued.
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Get("/cached-resource"))
		if err != nil {
			b.Fatalf("cached Execute failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("unexpected status %d", resp.StatusCode)
		}
	}
}

// ---------------------------------------------------------------------------
// BenchmarkExecuteBatch_100
//
// Measures the throughput of ExecuteBatch dispatching 100 requests with a
// concurrency limit of 10. This exercises the semaphore, goroutine fan-out,
// and result aggregation code paths.
// ---------------------------------------------------------------------------
func BenchmarkExecuteBatch_100(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(10*time.Second),
	)

	const batchSize = 100
	const concurrency = 10

	// Build the request slice once; it is reused across iterations.
	requests := make([]*relay.Request, batchSize)
	for i := range requests {
		requests[i] = client.Get(fmt.Sprintf("/item/%d", i))
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Pre-enqueue one 200 OK per request in the batch.
		for j := 0; j < batchSize; j++ {
			srv.Enqueue(testutil.MockResponse{
				Status: http.StatusOK,
				Body:   fmt.Sprintf(`{"id":%d}`, j),
			})
		}

		results := client.ExecuteBatch(context.Background(), requests, concurrency)
		if len(results) != batchSize {
			b.Fatalf("expected %d results, got %d", batchSize, len(results))
		}
		for _, r := range results {
			if r.Err != nil {
				b.Fatalf("batch item %d failed: %v", r.Index, r.Err)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// BenchmarkExecuteAsync
//
// Measures fire-and-forget throughput. We launch N goroutines, each calling
// ExecuteAsync, then wait for all channels to drain. This exercises the
// goroutine dispatch, buffered channel allocation, and the in-flight counter.
// ---------------------------------------------------------------------------
func BenchmarkExecuteAsync(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(5*time.Second),
	)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		const fanOut = 10

		// Enqueue responses before launching goroutines.
		for j := 0; j < fanOut; j++ {
			srv.Enqueue(testutil.MockResponse{
				Status: http.StatusOK,
				Body:   `{"async":true}`,
			})
		}

		channels := make([]<-chan relay.AsyncResult, fanOut)
		for j := 0; j < fanOut; j++ {
			channels[j] = client.ExecuteAsync(client.Get("/async"))
		}

		// Collect all results, failing fast on any error.
		var wg sync.WaitGroup
		wg.Add(fanOut)
		for _, ch := range channels {
			ch := ch
			go func() {
				defer wg.Done()
				result := <-ch
				if result.Err != nil {
					b.Errorf("async Execute failed: %v", result.Err)
				}
			}()
		}
		wg.Wait()
	}
}

// ---------------------------------------------------------------------------
// BenchmarkExecute_NoTiming
//
// Measures the Execute pipeline with timing instrumentation disabled via
// [relay.WithDisableTiming]. Skipping httptrace avoids roughly 10 allocations
// per call (timingCollector, ClientTrace, 7 closures, context value). Compare
// against BenchmarkExecute_Simple to quantify the timing overhead.
// ---------------------------------------------------------------------------
func BenchmarkExecute_NoTiming(b *testing.B) {
srv := testutil.NewMockServer()
defer srv.Close()

client := relay.New(
relay.WithBaseURL(srv.URL()),
relay.WithDisableRetry(),
relay.WithDisableCircuitBreaker(),
relay.WithDisableTiming(),
relay.WithTimeout(5*time.Second),
)

b.ResetTimer()
b.ReportAllocs()

for i := 0; i < b.N; i++ {
srv.Enqueue(testutil.MockResponse{
Status: http.StatusOK,
Body:   `{"id":1,"name":"relay"}`,
})

resp, err := client.Execute(client.Get("/bench"))
if err != nil {
b.Fatalf("Execute failed: %v", err)
}
if resp.StatusCode != http.StatusOK {
b.Fatalf("unexpected status %d", resp.StatusCode)
}
}
}
