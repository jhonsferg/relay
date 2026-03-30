package relay

import (
	"context"
	"sync"
)

// BatchResult holds the outcome of a single request within a batch.
type BatchResult struct {
	// Index is the position of this result in the original request slice,
	// allowing callers to correlate results with their requests.
	Index int
	// Response is the HTTP response, or nil if Err is non-nil.
	Response *Response
	// Err is the error returned by Execute, or nil on success.
	Err error
}

// ExecuteBatch sends all requests concurrently and returns their results in
// the same order as the input slice. maxConcurrency caps the number of
// simultaneous in-flight requests; pass 0 or a negative value for fully
// parallel execution (len(requests) workers).
//
// ctx governs how long to wait for a concurrency slot. Individual request
// timeouts and contexts are set on each [Request] via [Request.WithTimeout]
// or [Request.WithContext] — they are independent of the batch ctx.
//
// All results are populated before ExecuteBatch returns; no result slot is
// ever left at its zero value.
//
//	results := client.ExecuteBatch(ctx, []*httpclient.Request{
//	    client.Get("/users/1"),
//	    client.Get("/users/2"),
//	    client.Get("/users/3"),
//	}, 5)
//
//	for _, r := range results {
//	    if r.Err != nil { ... }
//	    fmt.Println(r.Index, r.Response.StatusCode)
//	}
func (c *Client) ExecuteBatch(ctx context.Context, requests []*Request, maxConcurrency int) []BatchResult {
	n := len(requests)
	if n == 0 {
		return nil
	}

	if maxConcurrency <= 0 || maxConcurrency > n {
		maxConcurrency = n
	}

	results := make([]BatchResult, n)
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, req := range requests {
		// Short-circuit remaining requests when the batch context is already done.
		if ctx.Err() != nil {
			mu.Lock()
			results[i] = BatchResult{Index: i, Err: ctx.Err()}
			mu.Unlock()
			continue
		}

		wg.Add(1)
		i, req := i, req // capture loop variables
		go func() {
			defer wg.Done()

			// Acquire a concurrency slot, respecting the batch context.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				mu.Lock()
				results[i] = BatchResult{Index: i, Err: ctx.Err()}
				mu.Unlock()
				return
			}
			defer func() { <-sem }()

			resp, err := c.Execute(req)
			mu.Lock()
			results[i] = BatchResult{Index: i, Response: resp, Err: err}
			mu.Unlock()
		}()
	}

	wg.Wait()
	return results
}
