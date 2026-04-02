package relay

import (
	"net/http"
	"net/url"
	"testing"
)

// BenchmarkGenerateIdempotencyKey measures allocation cost of UUID v4 generation.
func BenchmarkGenerateIdempotencyKey(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = generateIdempotencyKey()
	}
}

// BenchmarkCoalesceKey_NoAuth measures key building when no auth headers are set.
func BenchmarkCoalesceKey_NoAuth(b *testing.B) {
	req := &http.Request{
		Method: http.MethodGet,
		URL:    mustParseURL("https://api.example.com/v1/users?page=1"),
		Header: make(http.Header),
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = coalesceKey(req)
	}
}

// BenchmarkCoalesceKey_WithAuth measures key building when Authorization is present.
func BenchmarkCoalesceKey_WithAuth(b *testing.B) {
	req := &http.Request{
		Method: http.MethodGet,
		URL:    mustParseURL("https://api.example.com/v1/users?page=1"),
		Header: http.Header{
			"Authorization":   {"Bearer eyJhbGciOiJSUzI1NiJ9.test"},
			"Accept":          {"application/json"},
			"Accept-Language": {"en-US,en;q=0.9"},
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = coalesceKey(req)
	}
}

// BenchmarkRequestBuild_NoQuery measures Request.build() with a plain path and no query params.
func BenchmarkRequestBuild_NoQuery(b *testing.B) {
	client := New(WithBaseURL("https://api.example.com"))
	req := client.Get("/v1/users/42")
	parsedBase := mustParseURL("https://api.example.com")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = req.build("https://api.example.com", parsedBase, NormalizationAuto)
	}
}

// BenchmarkRequestBuild_WithQuery measures Request.build() with 3 query parameters.
func BenchmarkRequestBuild_WithQuery(b *testing.B) {
	client := New(WithBaseURL("https://api.example.com"))
	req := client.Get("/v1/search").
		WithQueryParam("q", "relay").
		WithQueryParam("page", "1").
		WithQueryParam("limit", "20")
	parsedBase := mustParseURL("https://api.example.com")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = req.build("https://api.example.com", parsedBase, NormalizationAuto)
	}
}

func mustParseURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}
