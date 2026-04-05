package relay_test

import (
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/testutil"
)

func TestDeduplication_ConcurrentGETs(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	const responseBody = "hello deduplicated world"
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: responseBody})

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithRequestDeduplication(),
		relay.WithDisableCircuitBreaker(),
	)

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)

	type res struct {
		body string
		err  error
	}
	results := make([]res, n)
	start := make(chan struct{})

	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			resp, err := client.Execute(client.Get("/ping"))
			if err != nil {
				results[i] = res{err: err}
				return
			}
			results[i] = res{body: resp.String()}
		}()
	}
	close(start)
	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			t.Errorf("goroutine %d: unexpected error: %v", i, r.err)
			continue
		}
		if r.body != responseBody {
			t.Errorf("goroutine %d: body = %q, want %q", i, r.body, responseBody)
		}
	}

	if count := srv.RequestCount(); count != 1 {
		t.Errorf("expected 1 HTTP request, got %d", count)
	}
}

func TestDeduplication_DisabledByDefault(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	const n = 10
	for i := 0; i < n; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: fmt.Sprintf("r%d", i)})
	}

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableCircuitBreaker(),
	)

	var wg sync.WaitGroup
	wg.Add(n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, _ = client.Execute(client.Get("/ping"))
		}()
	}
	close(start)
	wg.Wait()

	if count := srv.RequestCount(); count != n {
		t.Errorf("expected %d HTTP requests, got %d", n, count)
	}
}

func TestDeduplication_POSTNotDeduplicated(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	const n = 5
	for i := 0; i < n; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})
	}

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithRequestDeduplication(),
		relay.WithDisableCircuitBreaker(),
	)

	var wg sync.WaitGroup
	wg.Add(n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, _ = client.Execute(client.Post("/data").WithBody([]byte(`{"x":1}`)))
		}()
	}
	close(start)
	wg.Wait()

	if count := srv.RequestCount(); count != n {
		t.Errorf("expected %d HTTP requests for POST, got %d", n, count)
	}
}

func TestDeduplication_DifferentURLsNotDeduplicated(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	const n = 4
	for i := 0; i < n; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: fmt.Sprintf("b%d", i)})
	}

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithRequestDeduplication(),
		relay.WithDisableCircuitBreaker(),
	)

	var wg sync.WaitGroup
	wg.Add(n)
	start := make(chan struct{})
	paths := []string{"/a", "/b", "/c", "/d"}
	for i := 0; i < n; i++ {
		path := paths[i]
		go func() {
			defer wg.Done()
			<-start
			_, _ = client.Execute(client.Get(path))
		}()
	}
	close(start)
	wg.Wait()

	if count := srv.RequestCount(); count != n {
		t.Errorf("expected %d HTTP requests for different URLs, got %d", n, count)
	}
}

func TestDeduplication_PerRequestOverrideDisable(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	const n = 3
	for i := 0; i < n; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "ok"})
	}

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithRequestDeduplication(),
		relay.WithDisableCircuitBreaker(),
	)

	var wg sync.WaitGroup
	wg.Add(n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, _ = client.Execute(client.Get("/ping").WithDeduplication(false))
		}()
	}
	close(start)
	wg.Wait()

	if count := srv.RequestCount(); count != n {
		t.Errorf("expected %d HTTP requests with dedup disabled, got %d", n, count)
	}
}

func TestDeduplication_BodyCorrectness(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	const responseBody = "the-golden-response-body"
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: responseBody})

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithRequestDeduplication(),
		relay.WithDisableCircuitBreaker(),
	)

	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	bodies := make([]string, n)
	start := make(chan struct{})

	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			resp, err := client.Execute(client.Get("/content"))
			if err != nil {
				return
			}
			bodies[i] = resp.String()
		}()
	}
	close(start)
	wg.Wait()

	for i, b := range bodies {
		if b != responseBody {
			t.Errorf("goroutine %d: body = %q, want %q", i, b, responseBody)
		}
	}
}
