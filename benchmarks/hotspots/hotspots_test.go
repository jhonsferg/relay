// Package hotspots contains targeted benchmarks that isolate individual
// allocation sources and performance hotspots within the relay HTTP client.
// These benchmarks are used to measure the impact of specific optimizations
// before and after implementation, enabling precise performance tuning.
package hotspots

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"

	relay "github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/testutil"
)

// BenchmarkHotspot_CacheKeyGeneration measures the cost of generating
// cache keys for the lookup table. Current implementation uses string
// concatenation (req.Method + ":" + req.URL.String()).
func BenchmarkHotspot_CacheKeyGeneration(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithCache(relay.NewInMemoryCacheStore(1000)),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusOK,
			Headers: map[string]string{
				"Cache-Control": "public, max-age=3600",
			},
			Body: `{"id":1}`,
		})

		resp, _ := client.Execute(client.Get("/api/users/1"))
		_ = resp
	}
}

// BenchmarkHotspot_ContextWithTimeout measures the overhead of creating
// timeout contexts when a request has a custom timeout set.
func BenchmarkHotspot_ContextWithTimeout(b *testing.B) {
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
			Body:   `{"id":1}`,
		})

		// Each request creates a new timeout context
		req := client.Get("/bench").WithTimeout(5000) // 5 second timeout
		resp, _ := client.Execute(req)
		_ = resp
	}
}

// BenchmarkHotspot_ResponseBodyCopy measures the cost of copying the
// response body from the pooled buffer to a new allocation. This happens
// on every response, even for empty bodies.
func BenchmarkHotspot_ResponseBodyCopy(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	smallBody := `{"status":"ok"}`

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusOK,
			Body:   smallBody,
		})

		resp, _ := client.Execute(client.Get("/bench"))
		_ = resp
	}
}

// BenchmarkHotspot_PathParamSubstitution measures the cost of substituting
// path parameters in the URL template. Current implementation uses
// strings.ReplaceAll for each parameter.
func BenchmarkHotspot_PathParamSubstitution(b *testing.B) {
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
			Body:   `{"id":1}`,
		})

		// Multiple path parameters to exercise substitution
		req := client.Get("/api/v1/orgs/{orgId}/teams/{teamId}/members/{userId}").
			WithPathParam("orgId", "org-123").
			WithPathParam("teamId", "team-456").
			WithPathParam("userId", "user-789")

		resp, _ := client.Execute(req)
		_ = resp
	}
}

// BenchmarkHotspot_QueryParamEncoding measures the cost of encoding
// query parameters into the URL string.
func BenchmarkHotspot_QueryParamEncoding(b *testing.B) {
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
			Body:   `[]`,
		})

		req := client.Get("/api/users").
			WithQueryParam("limit", "50").
			WithQueryParam("offset", "100").
			WithQueryParam("sort", "created_at").
			WithQueryParam("filter", "active")

		resp, _ := client.Execute(req)
		_ = resp
	}
}

// BenchmarkHotspot_HeaderCloning measures the cost of cloning response
// headers. This happens during cache storage and cache replay operations.
func BenchmarkHotspot_HeaderCloning(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithCache(relay.NewInMemoryCacheStore(1000)),
	)

	headers := map[string]string{
		"Cache-Control":   "public, max-age=3600",
		"Content-Type":    "application/json",
		"X-Custom-Header": "value1",
		"X-Another-Header": "value2",
		"X-More-Headers":  "value3",
		"X-Trace-ID":      "abc123def456",
		"X-Request-ID":    "req-789",
		"Vary":            "Accept-Encoding",
		"ETag":            `"abc123"`,
		"Last-Modified":   "Mon, 01 Jan 2024 00:00:00 GMT",
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{
			Status:  http.StatusOK,
			Headers: headers,
			Body:    `{"id":1}`,
		})

		resp, _ := client.Execute(client.Get("/api/data"))
		_ = resp
	}
}

// BenchmarkHotspot_CacheControlParsing measures the cost of parsing
// Cache-Control header values. This happens every time a response is
// evaluated for caching.
func BenchmarkHotspot_CacheControlParsing(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithCache(relay.NewInMemoryCacheStore(1000)),
	)

	complexCacheControl := "public, max-age=3600, s-maxage=7200, must-revalidate, proxy-revalidate"

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusOK,
			Headers: map[string]string{
				"Cache-Control": complexCacheControl,
			},
			Body: `{"id":1}`,
		})

		resp, _ := client.Execute(client.Get("/api/resource"))
		_ = resp
	}
}

// BenchmarkHotspot_URLBuilding_WithBaseURL measures the cost of building
// the final URL when a base URL and path are combined.
func BenchmarkHotspot_URLBuilding_WithBaseURL(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL() + "/api/v1"),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{
			Status: http.StatusOK,
			Body:   `{"id":1}`,
		})

		resp, _ := client.Execute(client.Get("/users/123/profile"))
		_ = resp
	}
}

// BenchmarkHotspot_EmptyResponseBody measures the allocation overhead for
// responses with no body content. This is important because empty responses
// should not trigger unnecessary allocations.
func BenchmarkHotspot_EmptyResponseBody(b *testing.B) {
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
			Status: http.StatusNoContent,
			Body:   "",
		})

		resp, _ := client.Execute(client.Delete("/users/123"))
		_ = resp
	}
}

// BenchmarkHotspot_URLValueEncoding measures the performance of the
// url.Values.Encode() function when building query strings.
func BenchmarkHotspot_URLValueEncoding(b *testing.B) {
	values := url.Values{
		"param1": {"value1"},
		"param2": {"value with spaces"},
		"param3": {"value/with/slashes"},
		"param4": {"value?with=special&chars"},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = values.Encode()
	}
}

// BenchmarkHotspot_StringConcatenation measures the cost of string
// concatenation using the + operator versus alternative methods like
// strings.Builder.
func BenchmarkHotspot_StringConcatenation(b *testing.B) {
	method := "GET"
	url := "https://api.example.com/v1/users/123?filter=active"

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = method + ":" + url
	}
}

// BenchmarkHotspot_FormattedURL measures string operations needed to
// combine method and URL for cache key generation.
func BenchmarkHotspot_FormattedURL(b *testing.B) {
	method := "POST"
	url := "https://api.example.com/v1/users/create"

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("%s:%s", method, url)
	}
}

// BenchmarkHotspot_MultipleContextWrap measures the cost of wrapping
// a context multiple times with WithTimeout and WithValue operations.
func BenchmarkHotspot_MultipleContextWrap(b *testing.B) {
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
			Body:   `{"id":1}`,
		})

		// Request with custom timeout triggers context wrapping
		req := client.Get("/api/data").WithTimeout(5000)
		resp, _ := client.Execute(req)
		_ = resp
	}
}
