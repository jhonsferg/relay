// Package connection_pooling contains benchmarks focused on HTTP connection
// pool behavior, reuse patterns, and pool size impact on performance.
package connection_pooling

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/testutil"
)

// BenchmarkConnectionPooling_DefaultPool measures performance with relay's
// default connection pool configuration. This establishes baseline behavior.
func BenchmarkConnectionPooling_DefaultPool(b *testing.B) {
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
			Body:   `{"pool":"default"}`,
		})

		resp, _ := client.Execute(client.Get("/pool/default"))
		_ = resp
	}
}

// BenchmarkConnectionPooling_MinimalPool measures performance with a
// minimal pool size (1 connection). This simulates resource-constrained
// environments and establishes a baseline for pool contention.
func BenchmarkConnectionPooling_MinimalPool(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithConnectionPool(1, 1, 1), // min=1, max idle=1, max per host=1
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusOK,
			Body:   `{"pool":"minimal"}`,
		})

		resp, _ := client.Execute(client.Get("/pool/minimal"))
		_ = resp
	}
}

// BenchmarkConnectionPooling_OptimalPool measures performance with a
// carefully tuned pool size for typical workloads (50 max connections).
func BenchmarkConnectionPooling_OptimalPool(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithConnectionPool(50, 25, 50), // balanced configuration
	)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			srv.Enqueue(testutil.MockResponse{
				Status: http.StatusOK,
				Body:   `{"pool":"optimal"}`,
			})

			resp, _ := client.Execute(client.Get("/pool/optimal"))
			_ = resp
		}
	})
}

// BenchmarkConnectionPooling_AggressivePool measures performance with a
// very large pool (1000 connections). This tests overhead of large pool
// management and is suitable for extreme-scale scenarios.
func BenchmarkConnectionPooling_AggressivePool(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithConnectionPool(1000, 500, 1000), // aggressive sizing
	)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			srv.Enqueue(testutil.MockResponse{
				Status: http.StatusOK,
				Body:   `{"pool":"aggressive"}`,
			})

			resp, _ := client.Execute(client.Get("/pool/aggressive"))
			_ = resp
		}
	})
}

// BenchmarkConnectionPooling_ConnectionReuse measures the efficiency gain
// from connection reuse by making sequential requests to the same endpoint.
// Connection reuse should reduce latency significantly.
func BenchmarkConnectionPooling_ConnectionReuse(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithConnectionPool(10, 5, 10),
	)

	b.ReportAllocs()
	b.ResetTimer()

	// Make repeated requests to the same path to maximize connection reuse
	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusOK,
			Body:   `{"reuse":"connection"}`,
		})

		resp, _ := client.Execute(client.Get("/pool/reuse"))
		_ = resp
	}
}

// BenchmarkConnectionPooling_MultiHostRequests measures pool performance when
// requests are distributed across multiple hosts. This tests per-host pool
// management and connection load distribution.
func BenchmarkConnectionPooling_MultiHostRequests(b *testing.B) {
	servers := make([]*testutil.MockServer, 3)
	clients := make([]*relay.Client, 3)

	for i := 0; i < 3; i++ {
		servers[i] = testutil.NewMockServer()
		clients[i] = relay.New(
			relay.WithBaseURL(servers[i].URL()),
			relay.WithDisableRetry(),
			relay.WithDisableCircuitBreaker(),
			relay.WithConnectionPool(20, 10, 20),
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
		hostIdx := 0
		for pb.Next() {
			client := clients[hostIdx%len(clients)]
			srv := servers[hostIdx%len(servers)]

			srv.Enqueue(testutil.MockResponse{
				Status: http.StatusOK,
				Body:   fmt.Sprintf(`{"host":%d}`, hostIdx),
			})

			resp, _ := client.Execute(client.Get("/pool/multi"))
			_ = resp
			hostIdx++
		}
	})
}

// BenchmarkConnectionPooling_IdleConnTimeout measures the impact of idle
// connection timeout on performance. Shorter timeouts increase connection
// churn but reduce resource consumption.
func BenchmarkConnectionPooling_IdleConnTimeout(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithConnectionPool(20, 10, 20),
		relay.WithIdleConnTimeout(100*time.Millisecond), // short timeout
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusOK,
			Body:   `{"idle":"timeout"}`,
		})

		resp, _ := client.Execute(client.Get("/pool/idle"))
		_ = resp
	}
}

// BenchmarkConnectionPooling_KeepAliveDisabled measures performance with
// HTTP Keep-Alive disabled. Each request requires a new connection,
// increasing latency and connection overhead significantly.
func BenchmarkConnectionPooling_KeepAliveDisabled(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	tr := &http.Transport{
		DisableKeepAlives: true,
		MaxIdleConnsPerHost: 0,
		Dial: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 0,
		}).Dial,
	}

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTransportMiddleware(func(rt http.RoundTripper) http.RoundTripper {
			return tr
		}),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusOK,
			Body:   `{"keepalive":"disabled"}`,
		})

		resp, _ := client.Execute(client.Get("/pool/nokeepalive"))
		_ = resp
	}
}

// BenchmarkConnectionPooling_PoolExhaustion measures behavior when the
// connection pool is exhausted under high concurrent load. This tests
// queue blocking and recovery patterns.
func BenchmarkConnectionPooling_PoolExhaustion(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithConnectionPool(5, 2, 5), // very small pool to force contention
	)

	b.ReportAllocs()
	b.ResetTimer()

	var wg sync.WaitGroup
	burstSize := 100
	iterations := b.N / burstSize

	for i := 0; i < iterations; i++ {
		for j := 0; j < burstSize; j++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				srv.Enqueue(testutil.MockResponse{
					Status: http.StatusOK,
					Body:   fmt.Sprintf(`{"exhaustion":%d}`, idx),
				})

				resp, _ := client.Execute(client.Get(fmt.Sprintf("/pool/exhaust/%d", idx)))
				_ = resp
			}(j)
		}
		wg.Wait()
	}
}
