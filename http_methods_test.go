package relay

import (
	"net/http"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

func TestClient_Put(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	resp, err := c.Execute(c.Put(srv.URL() + "/resource"))
	if err != nil {
		t.Fatalf("Execute PUT: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Method != http.MethodPut {
		t.Errorf("expected PUT, got %s", rec.Method)
	}
}

func TestClient_Patch(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	resp, err := c.Execute(c.Patch(srv.URL() + "/resource"))
	if err != nil {
		t.Fatalf("Execute PATCH: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Method != http.MethodPatch {
		t.Errorf("expected PATCH, got %s", rec.Method)
	}
}

func TestClient_Delete(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusNoContent})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	resp, err := c.Execute(c.Delete(srv.URL() + "/resource"))
	if err != nil {
		t.Fatalf("Execute DELETE: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Method != http.MethodDelete {
		t.Errorf("expected DELETE, got %s", rec.Method)
	}
}

func TestClient_Head(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	resp, err := c.Execute(c.Head(srv.URL() + "/resource"))
	if err != nil {
		t.Fatalf("Execute HEAD: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Method != http.MethodHead {
		t.Errorf("expected HEAD, got %s", rec.Method)
	}
}

func TestClient_Options(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	resp, err := c.Execute(c.Options(srv.URL() + "/resource"))
	if err != nil {
		t.Fatalf("Execute OPTIONS: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Method != http.MethodOptions {
		t.Errorf("expected OPTIONS, got %s", rec.Method)
	}
}

func TestClient_CloseIdleConnections(t *testing.T) {
	t.Parallel()
	c := New()
	// Should not panic.
	c.CloseIdleConnections()
}
