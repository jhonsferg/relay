// Package bigdata contains benchmarks for handling large payloads and
// high-volume data transfers with the relay HTTP client. These benchmarks
// simulate real-world scenarios with thousands of records and multi-megabyte
// response bodies, testing memory efficiency and throughput under data-intensive workloads.
package bigdata

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
)

// HeavyRecord represents a single record in heavy-weight benchmark scenarios
type HeavyRecord struct {
	ID        int       `json:"id"`
	UUID      string    `json:"uuid"`
	Payload   string    `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
	Active    bool      `json:"active"`
}

type HeavyResponse struct {
	Total   int           `json:"total"`
	Data    []HeavyRecord `json:"data"`
	Version string        `json:"version"`
}

// SetupHeavyServer creates a test HTTP server that returns a massive JSON response.
// The 'count' parameter defines how many records to include in the Data slice.
func SetupHeavyServer(count int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"total":%d,"version":"2.0","data":[`, count) //nolint:errcheck
		for i := 0; i < count; i++ {
			if i > 0 {
				fmt.Fprint(w, ",") //nolint:errcheck
			}
			fmt.Fprintf(w, `{"id":%d,"uuid":"550e8400-e29b-41d4-a716-446655440000","payload":"lorem ipsum dolor sit amet consectetur adipiscing elit","timestamp":"2026-03-30T15:00:00Z","active":true}`, i) //nolint:errcheck
		}
		fmt.Fprint(w, `]}`) //nolint:errcheck
	}))
}

const (
	RecordsPerRequest = 50000 // 50k records per request (~7MB JSON)
)

// Benchmark: High concurrency + large payload (50K records per request)

func BenchmarkHeavy_Parallel_Standard(b *testing.B) {
	server := SetupHeavyServer(RecordsPerRequest)
	defer server.Close()

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 1000,
			IdleConnTimeout:     90 * time.Second,
		},
		Timeout: 30 * time.Second,
	}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			res, err := client.Get(server.URL)
			if err != nil {
				continue
			}

			var data HeavyResponse
			body, readErr := io.ReadAll(res.Body)
			_ = res.Body.Close() //nolint:errcheck
			if readErr != nil {
				continue
			}
			if err := json.Unmarshal(body, &data); err != nil {
				continue
			}

			if data.Total != RecordsPerRequest {
				b.Errorf("data mismatch: expected %d, got %d", RecordsPerRequest, data.Total)
			}
		}
	})
}

func BenchmarkHeavy_Parallel_Relay(b *testing.B) {
	server := SetupHeavyServer(RecordsPerRequest)
	defer server.Close()

	relayClient := relay.New(
		relay.WithBaseURL(server.URL),
		relay.WithTimeout(30*time.Second),
		relay.WithConnectionPool(1000, 1000, 1000),
		relay.WithDisableRetry(),
	)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			data, _, err := relay.ExecuteAs[HeavyResponse](relayClient, relayClient.Get("/"))
			if err != nil {
				continue
			}

			if data.Total != RecordsPerRequest {
				b.Errorf("data mismatch: expected %d, got %d", RecordsPerRequest, data.Total)
			}
		}
	})
}

// Benchmark: Memory stress with garbage collection pressure (100K records)

func BenchmarkMemoryStress_Relay(b *testing.B) {
	server := SetupHeavyServer(100000)
	defer server.Close()

	relayClient := relay.New(relay.WithBaseURL(server.URL))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = relay.ExecuteAs[HeavyResponse](relayClient, relayClient.Get("/"))

		if i%10 == 0 {
			runtime.GC()
		}
	}
}

// Benchmark: Small payloads with high concurrency (microservices scenario)

func BenchmarkSmallPayload_Parallel_Relay(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"total":1,"version":"2.0","data":[{"id":1,"uuid":"550e8400-e29b-41d4-a716-446655440000","payload":"test","timestamp":"2026-03-30T15:00:00Z","active":true}]}`) //nolint:errcheck
	}))
	defer server.Close()

	relayClient := relay.New(
		relay.WithBaseURL(server.URL),
		relay.WithConnectionPool(1000, 1000, 1000),
		relay.WithDisableRetry(),
	)

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _, _ = relay.ExecuteAs[HeavyResponse](relayClient, relayClient.Get("/"))
		}
	})
}

// --- ESCENARIO: STREAMING DE DATOS MASIVOS ---
// Benchmark: Large stream handling (250K records, ~35MB JSON)

func BenchmarkLargeStream_Sequential_Relay(b *testing.B) {
	server := SetupHeavyServer(250000)
	defer server.Close()

	relayClient := relay.New(relay.WithBaseURL(server.URL))

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, _ = relay.ExecuteAs[HeavyResponse](relayClient, relayClient.Get("/"))
	}
}

// Benchmark: Connection reuse pattern (single connection reused in loop)

func BenchmarkConnectionReuse_Sequential_Relay(b *testing.B) {
	server := SetupHeavyServer(RecordsPerRequest)
	defer server.Close()

	relayClient := relay.New(
		relay.WithBaseURL(server.URL),
		relay.WithConnectionPool(1, 1, 100),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, _ = relay.ExecuteAs[HeavyResponse](relayClient, relayClient.Get("/"))
	}
}

// Benchmark: Allocation profile comparison (Relay vs net/http)

func BenchmarkAllocationProfile_Standard(b *testing.B) {
	server := SetupHeavyServer(RecordsPerRequest)
	defer server.Close()

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 1000,
		},
		Timeout: 30 * time.Second,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		res, _ := client.Get(server.URL)
		body, _ := io.ReadAll(res.Body)
		var data HeavyResponse
		_ = json.Unmarshal(body, &data)
		_ = res.Body.Close() //nolint:errcheck
	}
}

func BenchmarkAllocationProfile_Relay(b *testing.B) {
	server := SetupHeavyServer(RecordsPerRequest)
	defer server.Close()

	relayClient := relay.New(relay.WithBaseURL(server.URL))

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, _ = relay.ExecuteAs[HeavyResponse](relayClient, relayClient.Get("/"))
	}
}

// Benchmark: Idle connection cleanup with burst traffic pattern

func BenchmarkIdleConnections_Relay(b *testing.B) {
	server := SetupHeavyServer(10000)
	defer server.Close()

	relayClient := relay.New(
		relay.WithBaseURL(server.URL),
		relay.WithConnectionPool(500, 500, 500),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, _ = relay.ExecuteAs[HeavyResponse](relayClient, relayClient.Get("/"))

		// Every 100 requests, simulate idle period
		if i%100 == 99 {
			time.Sleep(100 * time.Millisecond)
		}
	}
}

/*
PERFORMANCE NOTES:

1. Memory Management: ExecuteAs internally uses an optimised buffer
   to minimise reallocations when reading the response body. For 50k+
   records, this helps keep heap fragmentation under control.

2. Concurrency: RunParallel tests mutex contention within the client.
   Relay delegates pool management to net/http but wraps it in a layer
   that prevents file descriptor leaks.

3. JSON Marshalling: The dominant cost in all cases is json.Unmarshal.
   Relay's advantage is ergonomic - it handles these volumes with a fraction
   of the code, reducing the error surface for resource management.

4. Connection Pooling: Relay internally optimises TCP connection reuse,
   reducing handshake overhead in high-concurrency scenarios.

5. GC Pressure: With buffer pooling and optimised structure layout,
   Relay significantly reduces garbage collector pressure - critical
   for applications serving millions of requests per second.
*/
