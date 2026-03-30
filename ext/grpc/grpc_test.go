package grpc_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
	relaygrpc "github.com/jhonsferg/relay/ext/grpc"
)

// captureHeaders returns an httptest.Server that captures the headers from the
// first request and signals via a channel.
func captureHeadersServer() (*httptest.Server, <-chan http.Header) {
	ch := make(chan http.Header, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case ch <- r.Header.Clone():
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	return srv, ch
}

func TestWithMetadata_SetsHeader(t *testing.T) {
	srv, headers := captureHeadersServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relaygrpc.WithMetadata("x-request-id", "req-123"),
	)

	if _, err := client.Execute(client.Get("/")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := <-headers
	want := "req-123"
	if v := got.Get("Grpc-Metadata-X-Request-Id"); v != want {
		t.Errorf("header = %q, want %q", v, want)
	}
}

func TestWithBinaryMetadata_Base64Encoded(t *testing.T) {
	srv, headers := captureHeadersServer()
	defer srv.Close()

	payload := []byte("binary\x00data")
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relaygrpc.WithBinaryMetadata("token", payload),
	)

	if _, err := client.Execute(client.Get("/")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := <-headers
	encoded := base64.StdEncoding.EncodeToString(payload)
	// Header name has -Bin suffix (case-insensitive).
	found := false
	for key, vals := range got {
		if strings.EqualFold(key, "Grpc-Metadata-Token-Bin") {
			if len(vals) > 0 && vals[0] == encoded {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("binary metadata header not found or wrong value. headers: %v", got)
	}
}

func TestWithTimeoutHeader_WithDeadline(t *testing.T) {
	srv, headers := captureHeadersServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relaygrpc.WithTimeoutHeader(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := client.Get("/").WithContext(ctx)
	if _, err := client.Execute(req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := <-headers
	v := got.Get("Grpc-Timeout")
	if v == "" {
		t.Fatal("Grpc-Timeout header missing")
	}
	// Should be in seconds format "XS" since we gave 5s.
	if !strings.HasSuffix(v, "S") && !strings.HasSuffix(v, "m") {
		t.Errorf("Grpc-Timeout = %q, want a duration string (e.g. 5S or 4999m)", v)
	}
}

func TestWithTimeoutHeader_NoDeadline(t *testing.T) {
	srv, headers := captureHeadersServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relaygrpc.WithTimeoutHeader(),
	)

	if _, err := client.Execute(client.Get("/")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := <-headers
	if v := got.Get("Grpc-Timeout"); v != "" {
		t.Errorf("Grpc-Timeout should be absent without deadline, got %q", v)
	}
}

func TestSetMetadata_SingleRequest(t *testing.T) {
	srv, headers := captureHeadersServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
	)

	req := relaygrpc.SetMetadata("x-trace-id", "trace-abc")(client.Get("/"))
	if _, err := client.Execute(req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := <-headers
	if v := got.Get("Grpc-Metadata-X-Trace-Id"); v != "trace-abc" {
		t.Errorf("header = %q, want %q", v, "trace-abc")
	}
}

func TestSetBinaryMetadata_SingleRequest(t *testing.T) {
	srv, headers := captureHeadersServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
	)

	data := []byte("hello")
	req := relaygrpc.SetBinaryMetadata("sig", data)(client.Post("/"))
	if _, err := client.Execute(req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := <-headers
	expected := base64.StdEncoding.EncodeToString(data)
	found := false
	for key, vals := range got {
		if strings.EqualFold(key, "Grpc-Metadata-Sig-Bin") {
			if len(vals) > 0 && vals[0] == expected {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("binary metadata not found. headers: %v", got)
	}
}

func TestParseMetadata(t *testing.T) {
	headers := map[string][]string{
		"Grpc-Metadata-X-Tenant-Id":  {"tenant-42"},
		"Grpc-Metadata-X-Role-Bin":   {base64.StdEncoding.EncodeToString([]byte("admin"))},
		"Content-Type":               {"application/json"},
		"Grpc-Metadata-Empty":        {},
	}

	result, err := relaygrpc.ParseMetadata(headers)
	if err != nil {
		t.Fatalf("ParseMetadata: %v", err)
	}

	if got := result["x-tenant-id"]; got != "tenant-42" {
		t.Errorf("x-tenant-id = %q, want %q", got, "tenant-42")
	}
	if got := result["x-role-bin"]; got != "admin" {
		t.Errorf("x-role-bin decoded = %q, want %q", got, "admin")
	}
	if _, ok := result["content-type"]; ok {
		t.Error("Content-Type should not appear in metadata result")
	}
}

func TestParseMetadata_InvalidBase64(t *testing.T) {
	headers := map[string][]string{
		"Grpc-Metadata-Bad-Bin": {"not!valid!base64!!!"},
	}
	_, err := relaygrpc.ParseMetadata(headers)
	if err == nil {
		t.Fatal("expected error for invalid base64, got nil")
	}
}

func TestMultipleMetadata(t *testing.T) {
	srv, headers := captureHeadersServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
		relaygrpc.WithMetadata("x-service", "payments"),
		relaygrpc.WithMetadata("x-version", "v2"),
	)

	if _, err := client.Execute(client.Get("/")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got := <-headers
	if v := got.Get("Grpc-Metadata-X-Service"); v != "payments" {
		t.Errorf("x-service = %q, want %q", v, "payments")
	}
	if v := got.Get("Grpc-Metadata-X-Version"); v != "v2" {
		t.Errorf("x-version = %q, want %q", v, "v2")
	}
}
