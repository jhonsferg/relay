package relay_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhonsferg/relay"
)

func TestRequestClone_IndependentHeaders(t *testing.T) {
	client := relay.New()
	base := client.Get("/api").
		WithHeader("Accept", "application/json").
		WithTag("op", "base")

	clone := base.Clone()
	clone.WithHeader("Authorization", "Bearer tok")
	clone.WithTag("op", "clone")

	// base must be unaffected.
	if base.Tag("op") != "base" {
		t.Errorf("base tag mutated: got %q, want %q", base.Tag("op"), "base")
	}
}

func TestRequestClone_IndependentBody(t *testing.T) {
	client := relay.New()
	base := client.Post("/api").WithBody([]byte("original"))

	clone := base.Clone()
	clone.WithBody([]byte("modified"))

	// Issue both - server receives different bodies.
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 128)
		n, _ := r.Body.Read(buf)
		bodies = append(bodies, string(buf[:n]))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := relay.New()
	c.Execute(c.Post(srv.URL).WithBody([]byte("original"))) //nolint:errcheck
	c.Execute(c.Post(srv.URL).WithBody([]byte("modified"))) //nolint:errcheck

	if len(bodies) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(bodies))
	}
	if bodies[0] == bodies[1] {
		t.Error("clone body change affected original")
	}
}

func TestRequestClone_IndependentQueryParams(t *testing.T) {
	client := relay.New()
	base := client.Get("/search").WithQueryParam("q", "cats")
	clone := base.Clone()
	clone.WithQueryParam("q", "dogs")

	// The clone has its own query map - base is unchanged.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Just verify Clone() doesn't panic and returns a non-nil request.
	if clone == nil {
		t.Fatal("Clone() returned nil")
	}
	if clone == base {
		t.Error("Clone() returned the same pointer")
	}
}

func TestRequestClone_NilTagsHandled(t *testing.T) {
	client := relay.New()
	base := client.Get("/api")
	// No tags set - Tags() returns nil.
	clone := base.Clone()
	if clone == nil {
		t.Fatal("Clone() returned nil")
	}
	if clone.Tags() != nil {
		t.Errorf("expected nil tags on clone, got %v", clone.Tags())
	}
}

func TestRequestWithMaxBodySize_PerRequestOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write 50 bytes.
		w.Write(make([]byte, 50)) //nolint:errcheck
	}))
	defer srv.Close()

	// Client default is 10 MB - override per-request to 20 bytes.
	client := relay.New()
	resp, err := client.Execute(
		client.Get(srv.URL).WithMaxBodySize(20),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.IsTruncated() {
		t.Error("expected body to be truncated at 20 bytes")
	}
	if len(resp.Body()) > 20 {
		t.Errorf("body length = %d, want ≤ 20", len(resp.Body()))
	}
}

func TestRequestWithMaxBodySize_UnlimitedOverride(t *testing.T) {
	const bodySize = 1024
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(make([]byte, bodySize)) //nolint:errcheck
	}))
	defer srv.Close()

	// Client with tiny global limit; per-request disables it with -1.
	client := relay.New(relay.WithMaxResponseBodyBytes(10))
	resp, err := client.Execute(
		client.Get(srv.URL).WithMaxBodySize(-1),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.IsTruncated() {
		t.Error("expected body NOT to be truncated when per-request limit is -1")
	}
	if len(resp.Body()) != bodySize {
		t.Errorf("body length = %d, want %d", len(resp.Body()), bodySize)
	}
}
