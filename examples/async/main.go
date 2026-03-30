// Package main demonstrates relay's async execution patterns:
//   - ExecuteAsync   - returns a channel; caller selects on it
//   - ExecuteAsyncCallback - fires callbacks on success or error
//
// Async execution is useful for fire-and-forget notifications, fan-out
// parallelism where you want fine-grained control over each result channel,
// and integrating relay into event-driven architectures.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	relay "github.com/jhonsferg/relay"
)

func main() {
	// -------------------------------------------------------------------------
	// Test server: variable latency + occasional errors.
	// -------------------------------------------------------------------------
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fast":
			fmt.Fprint(w, `{"endpoint":"fast"}`)
		case "/slow":
			time.Sleep(120 * time.Millisecond)
			fmt.Fprint(w, `{"endpoint":"slow"}`)
		case "/error":
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"error":"internal"}`)
		default:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"path":%q}`, r.URL.Path)
		}
	}))
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDisableRetry(),
	)

	// -------------------------------------------------------------------------
	// 1. ExecuteAsync - single fire-and-forget with a timeout.
	//
	// ExecuteAsync returns a buffered channel that delivers exactly one result.
	// Use a select to impose an external deadline without canceling the request.
	// -------------------------------------------------------------------------
	fmt.Println("=== ExecuteAsync - single request with timeout ===")

	ch := client.ExecuteAsync(client.Get("/fast"))

	select {
	case result := <-ch:
		if result.Err != nil {
			log.Printf("request failed: %v", result.Err)
		} else {
			fmt.Printf("  status: %d  body: %s\n\n", result.Response.StatusCode, result.Response.String())
		}
	case <-time.After(500 * time.Millisecond):
		fmt.Print("  request timed out at the caller side\n\n")
	}

	// -------------------------------------------------------------------------
	// 2. Fan-out - launch multiple requests concurrently, collect in order.
	//
	// Each ExecuteAsync call returns its own channel. Collecting them in a
	// slice preserves the original request order in the output.
	// -------------------------------------------------------------------------
	fmt.Println("=== Fan-out - 5 concurrent requests ===")

	endpoints := []string{"/users/1", "/users/2", "/users/3", "/users/4", "/users/5"}
	channels := make([]<-chan relay.AsyncResult, len(endpoints))
	for i, path := range endpoints {
		channels[i] = client.ExecuteAsync(client.Get(path))
	}

	start := time.Now()
	for i, ch := range channels {
		result := <-ch // blocks until this specific request completes
		if result.Err != nil {
			fmt.Printf("  [%d] error: %v\n", i, result.Err)
		} else {
			fmt.Printf("  [%d] %s → %d %s\n", i, endpoints[i], result.Response.StatusCode, result.Response.String())
		}
	}
	fmt.Printf("  total elapsed: %s (serial would take ~%dms)\n\n",
		time.Since(start).Round(time.Millisecond), len(endpoints)*50)

	// -------------------------------------------------------------------------
	// 3. First-to-respond wins (speculative execution).
	//
	// Send the same request to two endpoints (primary + fallback). Use the
	// first successful response and ignore the second.
	// -------------------------------------------------------------------------
	fmt.Println("=== First-to-respond wins ===")

	primary := client.ExecuteAsync(client.Get("/slow"))  // slow backend
	fallback := client.ExecuteAsync(client.Get("/fast")) // fast backup

	select {
	case r := <-primary:
		if r.Err == nil && r.Response.IsSuccess() {
			fmt.Printf("  primary responded first: %s\n\n", r.Response.String())
		}
	case r := <-fallback:
		if r.Err == nil && r.Response.IsSuccess() {
			fmt.Printf("  fallback responded first: %s\n\n", r.Response.String())
		}
	case <-time.After(2 * time.Second):
		fmt.Print("  both timed out\n\n")
	}

	// -------------------------------------------------------------------------
	// 4. ExecuteAsyncCallback - callbacks on success and error.
	//
	// Useful for fire-and-forget event patterns where you do not need to wait
	// for the result in the calling goroutine.
	// -------------------------------------------------------------------------
	fmt.Println("=== ExecuteAsyncCallback - success callback ===")

	var cbWg sync.WaitGroup

	cbWg.Add(1)
	client.ExecuteAsyncCallback(
		client.Get("/fast"),
		func(resp *relay.Response) {
			defer cbWg.Done()
			fmt.Printf("  onSuccess: status=%d body=%s\n", resp.StatusCode, resp.String())
		},
		func(err error) {
			defer cbWg.Done()
			fmt.Printf("  onError: %v\n", err)
		},
	)
	cbWg.Wait()

	// -------------------------------------------------------------------------
	// 5. ExecuteAsyncCallback - error callback.
	// -------------------------------------------------------------------------
	fmt.Println("\n=== ExecuteAsyncCallback - HTTP error callback ===")

	cbWg.Add(1)
	client.ExecuteAsyncCallback(
		client.Get("/error"),
		func(resp *relay.Response) {
			defer cbWg.Done()
			// HTTP errors (4xx/5xx) are NOT transport errors - they land in
			// onSuccess. Inspect resp.IsError() to distinguish them.
			fmt.Printf("  onSuccess: status=%d (IsError=%v) body=%s\n",
				resp.StatusCode, resp.IsError(), resp.String())
		},
		func(err error) {
			defer cbWg.Done()
			fmt.Printf("  onError: %v\n", err)
		},
	)
	cbWg.Wait()

	// -------------------------------------------------------------------------
	// 6. Context-scoped async batch with early cancellation.
	//
	// Cancel the context after 60 ms - slow requests in-flight are abandoned
	// at the caller side (they may still complete in the background).
	// -------------------------------------------------------------------------
	fmt.Println("\n=== Context-scoped async batch with cancellation ===")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	paths := []string{"/fast", "/slow", "/fast", "/slow", "/fast"}
	chs := make([]<-chan relay.AsyncResult, len(paths))
	for i, p := range paths {
		chs[i] = client.ExecuteAsync(client.Get(p).WithContext(ctx))
	}

	completed, timedOut := 0, 0
	for i, ch := range chs {
		select {
		case r := <-ch:
			if r.Err != nil {
				timedOut++
				fmt.Printf("  [%d] %s → error: %v\n", i, paths[i], r.Err)
			} else {
				completed++
				fmt.Printf("  [%d] %s → %d\n", i, paths[i], r.Response.StatusCode)
			}
		case <-ctx.Done():
			timedOut++
			fmt.Printf("  [%d] %s → canceled by context\n", i, paths[i])
		}
	}
	fmt.Printf("  completed: %d  timed-out/canceled: %d\n\n", completed, timedOut)

	// -------------------------------------------------------------------------
	// 7. Structured result aggregation (map-reduce pattern).
	//
	// Fan-out to multiple API endpoints, collect into a typed result map.
	// -------------------------------------------------------------------------
	fmt.Println("=== Structured fan-out (map-reduce) ===")

	type ServiceResult struct {
		Name   string
		Status int
		Body   string
		Err    error
	}

	services := map[string]string{
		"users":    "/users/42",
		"orders":   "/orders/1",
		"products": "/products/7",
	}

	resultChs := make(map[string]<-chan relay.AsyncResult, len(services))
	for name, path := range services {
		resultChs[name] = client.ExecuteAsync(client.Get(path))
	}

	aggregated := make([]ServiceResult, 0, len(services))
	for name, ch := range resultChs {
		r := <-ch
		sr := ServiceResult{Name: name, Err: r.Err}
		if r.Err == nil {
			sr.Status = r.Response.StatusCode
			sr.Body = r.Response.String()
		}
		aggregated = append(aggregated, sr)
	}

	for _, sr := range aggregated {
		if sr.Err != nil {
			fmt.Printf("  %-10s → ERROR: %v\n", sr.Name, sr.Err)
		} else {
			fmt.Printf("  %-10s → %d  %s\n", sr.Name, sr.Status, sr.Body)
		}
	}
}
