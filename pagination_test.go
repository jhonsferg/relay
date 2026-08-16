package relay

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// TestPaginate_PreservesHeadersAcrossPages guards against a regression where
// every page after the first was built via c.Get(nextURL), discarding all
// customization on the original request - most importantly Authorization.
// Real paginated REST APIs virtually always require auth, so this silently
// broke pagination past page 1 for any authenticated endpoint.
func TestPaginate_PreservesHeadersAcrossPages(t *testing.T) {
	var mu sync.Mutex
	var seenAuth []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenAuth = append(seenAuth, r.Header.Get("Authorization"))
		n := len(seenAuth)
		mu.Unlock()

		if n < 3 {
			w.Header().Set("Link", fmt.Sprintf(`<%s/page%d>; rel="next"`, "http://"+r.Host, n+1))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := client.Get(srv.URL+"/page1").WithHeader("Authorization", "Bearer tok123")

	var pages int
	err := client.Paginate(context.Background(), req, func(resp *Response) (bool, error) {
		pages++
		return true, nil
	})
	if err != nil {
		t.Fatalf("Paginate: %v", err)
	}
	if pages != 3 {
		t.Fatalf("expected 3 pages, got %d", pages)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seenAuth) != 3 {
		t.Fatalf("expected 3 requests to reach the server, got %d", len(seenAuth))
	}
	for i, auth := range seenAuth {
		if auth != "Bearer tok123" {
			t.Errorf("page %d: Authorization = %q, want %q", i+1, auth, "Bearer tok123")
		}
	}
}

// TestPaginateWith_PreservesQueryParamsAcrossPages confirms query params set
// on the original request (e.g. a filter) persist onto every page, merging
// correctly with whatever query string nextFn's URL carries.
func TestPaginateWith_PreservesQueryParamsAcrossPages(t *testing.T) {
	var mu sync.Mutex
	var seenFilters []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seenFilters = append(seenFilters, r.URL.Query().Get("filter"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := client.Get(srv.URL+"/items").WithQueryParam("filter", "active")

	pageURLs := []string{srv.URL + "/items?page=2", ""}
	var call int
	nextFn := func(resp *Response) string {
		u := pageURLs[call]
		call++
		return u
	}

	var pages int
	err := client.PaginateWith(context.Background(), req, nextFn, func(resp *Response) (bool, error) {
		pages++
		return true, nil
	})
	if err != nil {
		t.Fatalf("PaginateWith: %v", err)
	}
	if pages != 2 {
		t.Fatalf("expected 2 pages, got %d", pages)
	}

	mu.Lock()
	defer mu.Unlock()
	for i, filter := range seenFilters {
		if filter != "active" {
			t.Errorf("page %d: filter query param = %q, want %q (lost across pagination)", i+1, filter, "active")
		}
	}
}

// TestPaginate_StopsOnNoLinkHeader confirms basic termination still works.
func TestPaginate_StopsOnNoLinkHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := New(WithDisableRetry(), WithDisableCircuitBreaker())
	var pages int
	err := client.Paginate(context.Background(), client.Get(srv.URL+"/"), func(resp *Response) (bool, error) {
		pages++
		return true, nil
	})
	if err != nil {
		t.Fatalf("Paginate: %v", err)
	}
	if pages != 1 {
		t.Errorf("expected 1 page (no Link header), got %d", pages)
	}
}
