package sentry_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// BenchmarkWithSentry_SuccessBreadcrumb measures the per-request overhead of
// scope configuration and breadcrumb recording on the common (200 OK) path.
func BenchmarkWithSentry_SuccessBreadcrumb(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ct := &captureTransport{}
	hub := newTestHub(ct)
	client := newRelayClient(srv, hub)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Get("/"))
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		_ = resp
	}
}

// BenchmarkWithSentry_5xxEvent measures the per-request overhead when every
// response also triggers an event capture (5xx path).
func BenchmarkWithSentry_5xxEvent(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ct := &captureTransport{}
	hub := newTestHub(ct)
	client := newRelayClient(srv, hub)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Get("/"))
		if err != nil {
			b.Fatalf("unexpected error: %v", err)
		}
		_ = resp
	}
}
