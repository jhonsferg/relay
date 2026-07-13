package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhonsferg/relay"
)

func TestMultiFlag_StringAndSet(t *testing.T) {
	t.Parallel()

	var m multiFlag
	if got := m.String(); got != "" {
		t.Errorf("String() on empty multiFlag = %q, want empty", got)
	}

	if err := m.Set("X-A: 1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := m.Set("X-B: 2"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	want := "X-A: 1, X-B: 2"
	if got := m.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestMakeFactory_GET(t *testing.T) {
	t.Parallel()

	var gotMethod, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Test")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New()
	defer func() { _ = client.Shutdown(context.Background()) }() //nolint:errcheck

	headers := multiFlag{"X-Test: hello"}
	factory := makeFactory(client, "GET", srv.URL, headers, "", "")

	req := factory()
	if req.Method() != "GET" {
		t.Errorf("Method() = %q, want GET", req.Method())
	}

	if _, err := client.Execute(req); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("server saw method %q, want GET", gotMethod)
	}
	if gotHeader != "hello" {
		t.Errorf("server saw X-Test = %q, want %q", gotHeader, "hello")
	}
}

func TestMakeFactory_MethodsAndBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method string
		want   string
	}{
		{method: "POST", want: http.MethodPost},
		{method: "PUT", want: http.MethodPut},
		{method: "PATCH", want: http.MethodPatch},
		{method: "DELETE", want: http.MethodDelete},
		{method: "GARBAGE", want: http.MethodGet}, // default branch
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			t.Parallel()

			var gotMethod string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			client := relay.New()
			defer func() { _ = client.Shutdown(context.Background()) }() //nolint:errcheck

			factory := makeFactory(client, tt.method, srv.URL, nil, "", "")
			if _, err := client.Execute(factory()); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if gotMethod != tt.want {
				t.Errorf("server saw method %q, want %q", gotMethod, tt.want)
			}
		})
	}
}

func TestMakeFactory_PlainBody(t *testing.T) {
	t.Parallel()

	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New()
	defer func() { _ = client.Shutdown(context.Background()) }() //nolint:errcheck

	factory := makeFactory(client, "POST", srv.URL, nil, "plain body content", "")
	if _, err := client.Execute(factory()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotBody != "plain body content" {
		t.Errorf("server saw body %q, want %q", gotBody, "plain body content")
	}
}

func TestMakeFactory_JSONBody(t *testing.T) {
	t.Parallel()

	var gotContentType string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New()
	defer func() { _ = client.Shutdown(context.Background()) }() //nolint:errcheck

	factory := makeFactory(client, "POST", srv.URL, nil, "", `{"key":"value"}`)
	if _, err := client.Execute(factory()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody["key"] != "value" {
		t.Errorf("body = %v, want key=value", gotBody)
	}
}

func TestMakeFactory_HeaderWithoutColonIsSkipped(t *testing.T) {
	t.Parallel()

	var seenHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New()
	defer func() { _ = client.Shutdown(context.Background()) }() //nolint:errcheck

	// A malformed header entry (no colon) must be silently ignored rather
	// than crashing the factory.
	factory := makeFactory(client, "GET", srv.URL, multiFlag{"not-a-valid-header"}, "", "")
	if _, err := client.Execute(factory()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if seenHeaders.Get("not-a-valid-header") != "" {
		t.Errorf("malformed header should not be set, got headers: %v", seenHeaders)
	}
}
