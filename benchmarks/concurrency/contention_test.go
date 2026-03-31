// Package concurrency contains benchmarks focused on concurrent request handling,
// goroutine contention, and parallel execution patterns in the relay HTTP client.
package concurrency

import (
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/testutil"
)

// BenchmarkConcurrency_ParallelRequests measures throughput when multiple
// goroutines issue concurrent requests to the same client. This tests
// internal mutex contention and pool efficiency.
func BenchmarkConcurrency_ParallelRequests(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			srv.Enqueue(testutil.MockResponse{
				Status: http.StatusOK,
				Body:   `{"parallel":true}`,
			})

			resp, _ := client.Execute(client.Get("/concurrent/parallel"))
			_ = resp
		}
	})
}

// BenchmarkConcurrency_SequentialBaseline establishes the baseline latency
// for a single goroutine making sequential requests, providing a point of
// comparison for concurrent overhead.
func BenchmarkConcurrency_SequentialBaseline(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusOK,
			Body:   `{"sequential":true}`,
		})

		resp, _ := client.Execute(client.Get("/concurrent/baseline"))
		_ = resp
	}
}

// BenchmarkConcurrency_HighContention measures client behavior under extreme
// concurrent load (thousands of goroutines). This tests how well the client
// scales with high CPU core counts and detects mutex bottlenecks.
func BenchmarkConcurrency_HighContention(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	b.ReportAllocs()
	b.ResetTimer()

	var wg sync.WaitGroup
	iterations := b.N / 100
	if iterations == 0 {
		iterations = 1
	}

	for i := 0; i < iterations; i++ {
		for j := 0; j < 100; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				srv.Enqueue(testutil.MockResponse{
					Status: http.StatusOK,
					Body:   `{"contention":"high"}`,
				})

				resp, _ := client.Execute(client.Get("/concurrent/high"))
				_ = resp
			}()
		}
		wg.Wait()
	}
}

// BenchmarkConcurrency_RateLimitedLoad measures performance when requests
// are rate-limited using relay's built-in rate limiter. This tests the
// overhead of throttling without reducing request volume artificially.
func BenchmarkConcurrency_RateLimitedLoad(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithRateLimit(10000, 100), // 10k req/s, burst of 100
	)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			srv.Enqueue(testutil.MockResponse{
				Status: http.StatusOK,
				Body:   `{"ratelimited":true}`,
			})

			resp, _ := client.Execute(client.Get("/concurrent/ratelimit"))
			_ = resp
		}
	})
}

// BenchmarkConcurrency_MultipleClients measures performance when using
// separate client instances across goroutines, avoiding shared client
// contention (best-case concurrent scenario).
func BenchmarkConcurrency_MultipleClients(b *testing.B) {
	servers := make([]*testutil.MockServer, 4)
	clients := make([]*relay.Client, 4)

	for i := 0; i < 4; i++ {
		servers[i] = testutil.NewMockServer()
		clients[i] = relay.New(
			relay.WithBaseURL(servers[i].URL()),
			relay.WithDisableRetry(),
			relay.WithDisableCircuitBreaker(),
		)
	}
	defer func() {
		for _, srv := range servers {
			srv.Close()
		}
	}()

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		clientIdx := uint32(0)
		for pb.Next() {
			idx := atomic.AddUint32(&clientIdx, 1) % uint32(len(clients))
			client := clients[idx]
			srv := servers[idx]

			srv.Enqueue(testutil.MockResponse{
				Status: http.StatusOK,
				Body:   `{"isolated":"client"}`,
			})

			resp, _ := client.Execute(client.Get("/concurrent/isolated"))
			_ = resp
		}
	})
}

// BenchmarkConcurrency_BurstTraffic measures how the client recovers from
// sudden spikes in concurrent request volume. This tests queue behavior and
// resource cleanup efficiency.
func BenchmarkConcurrency_BurstTraffic(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	b.ReportAllocs()
	b.ResetTimer()

	burstSize := 50
	iterations := b.N / burstSize

	for i := 0; i < iterations; i++ {
		var wg sync.WaitGroup

		// Burst: launch many goroutines at once
		for j := 0; j < burstSize; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				srv.Enqueue(testutil.MockResponse{
					Status: http.StatusOK,
					Body:   fmt.Sprintf(`{"burst":true,"iteration":%d}`, i),
				})

				resp, _ := client.Execute(client.Get(fmt.Sprintf("/concurrent/burst/%d", i)))
				_ = resp
			}()
		}

		wg.Wait()

		// Quiet period to allow cleanup
		time.Sleep(10 * time.Millisecond)
	}
}

// BenchmarkConcurrency_CircuitBreakerImpact measures the overhead imposed
// by the circuit breaker during normal operation (open state transitions).
func BenchmarkConcurrency_CircuitBreakerImpact(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
			MaxFailures:      5,
			ResetTimeout:     10 * time.Second,
			HalfOpenRequests: 3,
			SuccessThreshold: 2,
		}),
	)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			srv.Enqueue(testutil.MockResponse{
				Status: http.StatusOK,
				Body:   `{"circuitbreaker":"ok"}`,
			})

			resp, _ := client.Execute(client.Get("/concurrent/cb"))
			_ = resp
		}
	})
}
