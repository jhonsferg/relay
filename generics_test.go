package relay_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhonsferg/relay"
)

func TestExecuteAsStream_JSONL(t *testing.T) {
	type Item struct {
		N int `json:"n"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		for i := 1; i <= 5; i++ {
			b, _ := json.Marshal(Item{N: i})
			_, _ = fmt.Fprintf(w, "%s\n", b)
		}
	}))
	defer srv.Close()

	client := relay.New()
	var items []Item
	err := relay.ExecuteAsStream[Item](client, client.Get(srv.URL), func(item Item) bool {
		items = append(items, item)
		return true
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(items))
	}
	for i, item := range items {
		if item.N != i+1 {
			t.Errorf("items[%d].N = %d, want %d", i, item.N, i+1)
		}
	}
}

func TestExecuteAsStream_StopEarly(t *testing.T) {
	type Item struct {
		N int `json:"n"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 1; i <= 100; i++ {
			b, _ := json.Marshal(Item{N: i})
			_, _ = fmt.Fprintf(w, "%s\n", b)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}))
	defer srv.Close()

	client := relay.New()
	count := 0
	err := relay.ExecuteAsStream[Item](client, client.Get(srv.URL), func(item Item) bool {
		count++
		return count < 3
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 3 {
		t.Errorf("handler called %d times, want 3", count)
	}
}

func TestExecuteAsStream_BlankLinesSkipped(t *testing.T) {
	type Item struct {
		V string `json:"v"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"v":"a"}`+"\n\n"+`{"v":"b"}`+"\n")
	}))
	defer srv.Close()

	client := relay.New()
	var items []Item
	err := relay.ExecuteAsStream[Item](client, client.Get(srv.URL), func(item Item) bool {
		items = append(items, item)
		return true
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestExecuteAsStream_InvalidJSONReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "not-json\n")
	}))
	defer srv.Close()

	type Item struct{ V string }
	client := relay.New()
	err := relay.ExecuteAsStream[Item](client, client.Get(srv.URL), func(item Item) bool {
		return true
	})
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestExecuteAs_JSONResponse(t *testing.T) {
	type User struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":7,"name":"Alice"}`)
	}))
	defer srv.Close()

	client := relay.New()
	user, resp, err := relay.ExecuteAs[User](client, client.Get(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if user.ID != 7 || user.Name != "Alice" {
		t.Errorf("user = %+v, want {7 Alice}", user)
	}
}

// TestExecuteAs_WithResponseDecoder verifies that ExecuteAs uses the custom
// decoder when WithResponseDecoder is configured on the client.
func TestExecuteAs_WithResponseDecoder(t *testing.T) {
	t.Parallel()

	type User struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":42,"name":"Bob"}`)
	}))
	defer srv.Close()

	// Custom decoder that parses JSON but overrides Name.
	client := relay.New(
		relay.WithResponseDecoder(func(ct string, body []byte, v any) error {
			if err := json.Unmarshal(body, v); err != nil {
				return err
			}
			if u, ok := v.(*User); ok {
				u.Name = "decoded:" + u.Name
			}
			return nil
		}),
	)

	user, resp, err := relay.ExecuteAs[User](client, client.Get(srv.URL))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if user.ID != 42 || user.Name != "decoded:Bob" {
		t.Errorf("user = %+v, want {42 decoded:Bob}", user)
	}
}

// ---------------------------------------------------------------------------
// E4 - Generic response coercion
// ---------------------------------------------------------------------------

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":42,"name":"relay"}`))
	}))
	defer srv.Close()

	client := relay.New(relay.WithBaseURL(srv.URL))
	defer client.Shutdown(context.Background()) //nolint:errcheck

	resp, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	type Item struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	item, err := relay.DecodeJSON[Item](resp)
	if err != nil {
		t.Fatalf("DecodeJSON() error: %v", err)
	}
	if item.ID != 42 || item.Name != "relay" {
		t.Errorf("DecodeJSON() = %+v, want {ID:42 Name:relay}", item)
	}
}

func TestDecodeXML(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<item><ID>7</ID><Name>traverse</Name></item>`))
	}))
	defer srv.Close()

	client := relay.New(relay.WithBaseURL(srv.URL))
	defer client.Shutdown(context.Background()) //nolint:errcheck

	resp, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	type Item struct {
		ID   int    `xml:"ID"`
		Name string `xml:"Name"`
	}
	item, err := relay.DecodeXML[Item](resp)
	if err != nil {
		t.Fatalf("DecodeXML() error: %v", err)
	}
	if item.ID != 7 || item.Name != "traverse" {
		t.Errorf("DecodeXML() = %+v, want {ID:7 Name:traverse}", item)
	}
}

func TestDecodeAs_JSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":99}`))
	}))
	defer srv.Close()

	client := relay.New(relay.WithBaseURL(srv.URL))
	defer client.Shutdown(context.Background()) //nolint:errcheck

	resp, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	type Payload struct {
		Value int `json:"value"`
	}
	p, err := relay.DecodeAs[Payload](resp)
	if err != nil {
		t.Fatalf("DecodeAs() error: %v", err)
	}
	if p.Value != 99 {
		t.Errorf("DecodeAs() value = %d, want 99", p.Value)
	}
}

func TestResponseTextAndBytes(t *testing.T) {
	t.Parallel()

	body := "hello relay"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	client := relay.New(relay.WithBaseURL(srv.URL))
	defer client.Shutdown(context.Background()) //nolint:errcheck

	resp, err := client.Execute(client.Get("/"))
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if got := resp.Text(); got != body {
		t.Errorf("Text() = %q, want %q", got, body)
	}
	if got := string(resp.Bytes()); got != body {
		t.Errorf("Bytes() = %q, want %q", got, body)
	}
}
