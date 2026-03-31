// Package memory contains benchmarks focused on memory consumption,
// allocation patterns, and garbage collection pressure of the relay HTTP client.
package memory

import (
	"bytes"
	"fmt"
	"net/http"
	"runtime"
	"testing"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/testutil"
)

// BenchmarkMemory_SmallPayload_Allocation measures memory allocation overhead
// for requests with minimal payloads (< 1 KB), representing typical API responses.
// This establishes the baseline memory footprint per request.
func BenchmarkMemory_SmallPayload_Allocation(b *testing.B) {
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
			Body:   `{"id":1,"name":"test","value":42}`,
		})

		resp, _ := client.Execute(client.Get("/memory/small"))
		_ = resp
	}
}

// BenchmarkMemory_MediumPayload_Allocation measures allocation overhead with
// medium-sized JSON responses (10-100 KB), simulating paginated API responses.
func BenchmarkMemory_MediumPayload_Allocation(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	mediumBody := bytes.Repeat([]byte(`{"item":"data","value":123}`), 100)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusOK,
			Body:   string(mediumBody),
		})

		resp, _ := client.Execute(client.Get("/memory/medium"))
		_ = resp
	}
}

// BenchmarkMemory_LargePayload_Allocation measures allocation overhead when
// handling large responses (>= 1 MB). This tests the pooled buffer efficiency
// for large data transfers.
func BenchmarkMemory_LargePayload_Allocation(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	// Create a 1 MB response body
	largeBody := bytes.Repeat([]byte("X"), 1024*1024)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusOK,
			Body:   string(largeBody),
		})

		resp, _ := client.Execute(client.Get("/memory/large"))
		_ = resp
	}
}

// BenchmarkMemory_GCPressure_Forced measures allocation behaviour under
// repeated garbage collection cycles. This simulates high-frequency request
// scenarios where GC pressure impacts performance.
func BenchmarkMemory_GCPressure_Forced(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	smallBody := `{"result":"ok","data":{"nested":true,"value":999}}`

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusOK,
			Body:   smallBody,
		})

		resp, _ := client.Execute(client.Get("/memory/gc"))
		_ = resp

		// Force GC every iteration to measure behaviour under GC pressure
		runtime.GC()
	}
}

// BenchmarkMemory_Concurrent_Allocation measures allocation patterns when
// multiple goroutines issue concurrent requests. This tests memory pool
// efficiency and contention under concurrent load.
func BenchmarkMemory_Concurrent_Allocation(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	body := `{"concurrent":true,"goroutine":"allocation_test"}`

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			srv.Enqueue(testutil.MockResponse{
				Status: http.StatusOK,
				Body:   body,
			})

			resp, _ := client.Execute(client.Get("/memory/concurrent"))
			_ = resp
		}
	})
}

// BenchmarkMemory_ByteBuffer_Reuse measures the efficiency of internal
// buffer pooling by comparing sequential requests that can reuse buffers.
func BenchmarkMemory_ByteBuffer_Reuse(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	responseBody := bytes.Repeat([]byte("data"), 1000)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusOK,
			Body:   string(responseBody),
		})

		resp, _ := client.Execute(client.Get("/memory/reuse"))
		_ = resp
	}
}

// BenchmarkMemory_HeaderParsing_Overhead measures the allocation cost
// of parsing HTTP headers with varying complexity.
func BenchmarkMemory_HeaderParsing_Overhead(b *testing.B) {
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
			Headers: map[string]string{
				"Content-Type":      "application/json",
				"Cache-Control":     "max-age=3600, must-revalidate",
				"X-Custom-Header-1": "value1",
				"X-Custom-Header-2": "value2",
				"X-Custom-Header-3": "value3",
				"X-Request-ID":      fmt.Sprintf("req-%d", i),
				"X-Powered-By":      "relay",
				"Set-Cookie":        "session=abc123; Path=/; HttpOnly",
			},
			Body: `{"headers":"parsed"}`,
		})

		resp, _ := client.Execute(client.Get("/memory/headers"))
		_ = resp
	}
}
