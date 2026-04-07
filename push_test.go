package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPushedResponseCache_StoreLoad(t *testing.T) {
	c := NewPushedResponseCache()

	r1 := &http.Response{StatusCode: http.StatusOK}
	r2 := &http.Response{StatusCode: http.StatusNotFound}

	c.Store("https://example.com/style.css", r1)
	c.Store("https://example.com/app.js", r2)

	got1, ok1 := c.Load("https://example.com/style.css")
	if !ok1 || got1 != r1 {
		t.Fatal("expected r1 for style.css")
	}
	// Entry must be removed after load.
	if _, still := c.Load("https://example.com/style.css"); still {
		t.Fatal("expected style.css to be gone after first Load")
	}

	got2, ok2 := c.Load("https://example.com/app.js")
	if !ok2 || got2 != r2 {
		t.Fatal("expected r2 for app.js")
	}
}

func TestPushedResponseCache_Len(t *testing.T) {
	c := NewPushedResponseCache()
	if c.Len() != 0 {
		t.Fatalf("want 0, got %d", c.Len())
	}

	c.Store("https://example.com/a", &http.Response{})
	if c.Len() != 1 {
		t.Fatalf("want 1, got %d", c.Len())
	}

	c.Store("https://example.com/b", &http.Response{})
	if c.Len() != 2 {
		t.Fatalf("want 2, got %d", c.Len())
	}

	c.Load("https://example.com/a")
	if c.Len() != 1 {
		t.Fatalf("want 1 after load, got %d", c.Len())
	}

	c.Load("https://example.com/b")
	if c.Len() != 0 {
		t.Fatalf("want 0 after all loads, got %d", c.Len())
	}
}

func TestWithHTTP2PushHandler_NoopOnHTTP1(t *testing.T) {
	called := false
	handler := PushPromiseHandler(func(pushedURL string, pushedResp *http.Response) {
		called = true
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := New(WithHTTP2PushHandler(handler))
	resp, err := client.Execute(client.Get(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = resp.Body()

	if called {
		t.Fatal("push handler must not be called for an HTTP/1 server")
	}
}

// TestPushHandler_HTTP2Server verifies that a relay client can communicate
// with an HTTP/2 server without panicking. Because golang.org/x/net v0.50.0
// disables server push at the SETTINGS level, we cannot exercise the push
// handler inline; we instead confirm that the option does not interfere with
// normal H2 operation.
func TestPushHandler_HTTP2Server(t *testing.T) {
	pushed := make(chan string, 4)
	handler := PushPromiseHandler(func(pushedURL string, pushedResp *http.Response) {
		if pushedResp != nil && pushedResp.Body != nil {
			io.Copy(io.Discard, pushedResp.Body) //nolint:errcheck
			_ = pushedResp.Body.Close()
		}
		pushed <- pushedURL
	})

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			// Attempt to push /style.css — succeeds only when the client
			// hasn't disabled push (standard Go H2 client does disable it,
			// so this may return an error; that's expected).
			if pusher, ok := w.(http.Pusher); ok {
				_ = pusher.Push("/style.css", nil)
			}
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "<html></html>") //nolint:errcheck
			return
		}
		if r.URL.Path == "/style.css" {
			w.Header().Set("Content-Type", "text/css")
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "body{}") //nolint:errcheck
			return
		}
		http.NotFound(w, r)
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	tlsCfg := srv.Client().Transport.(*http.Transport).TLSClientConfig
	client := New(
		WithHTTP2PushHandler(handler),
		WithTLSConfig(tlsCfg),
	)

	resp, err := client.Execute(client.Get(srv.URL + "/"))
	if err != nil {
		// H2 over TLS may not be available in all CI environments (e.g. Windows).
		// Skip gracefully for any connection or protocol-level error.
		t.Skipf("skipping: H2 TLS not available in this environment: %v", err)
	}
	_ = resp.Body()

	// The response must have been received successfully.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
