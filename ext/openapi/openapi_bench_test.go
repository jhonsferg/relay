package openapi_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhonsferg/relay"
	relayopenapi "github.com/jhonsferg/relay/ext/openapi"
)

// BenchmarkRequestValidation_GET measures the per-request overhead of the
// validating transport's route matching + request validation for a simple
// GET request with no body.
func BenchmarkRequestValidation_GET(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	doc, err := relayopenapi.Load([]byte(petStoreSpec))
	if err != nil {
		b.Fatalf("Load spec: %v", err)
	}
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relayopenapi.WithValidation(doc),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Get("/pets"))
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		_ = resp
	}
}

// BenchmarkRequestValidation_UnknownRoute measures the overhead when the
// requested path isn't in the spec (FindRoute miss -> passthrough).
func BenchmarkRequestValidation_UnknownRoute(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	doc, err := relayopenapi.Load([]byte(petStoreSpec))
	if err != nil {
		b.Fatalf("Load spec: %v", err)
	}
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relayopenapi.WithValidation(doc),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Get("/unknown"))
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		_ = resp
	}
}
