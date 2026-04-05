package relay

import (
	"context"
	"sync"
	"time"
)

// hedgeResult is the outcome of one hedged request attempt.
type hedgeResult struct {
	resp *Response
	err  error
}

// executeHedged sends up to maxAttempts parallel requests, each delayed by
// after from the previous one. Returns the first successful response,
// or the last error if all fail.
func (c *Client) executeHedged(ctx context.Context, req *Request, after time.Duration, maxAttempts int) (*Response, error) {
	if maxAttempts <= 1 {
		maxAttempts = 2
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan hedgeResult, maxAttempts)
	var wg sync.WaitGroup

	for i := 0; i < maxAttempts; i++ {
		if i > 0 {
			// Wait before launching the next attempt, unless ctx is done.
			timer := time.NewTimer(after)
			select {
			case <-timer.C:
				// Delay elapsed; launch next attempt below.
			case <-ctx.Done():
				timer.Stop()
				// No more attempts; wait for results already in flight.
				goto collect
			case r := <-results:
				timer.Stop()
				// A result arrived while waiting; use it.
				cancel()
				if r.err == nil {
					return r.resp, nil
				}
				// Drain remaining results.
				wg.Wait()
				return r.resp, r.err
			}
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			// Clone the request so each goroutine has its own copy.
			cloned := req.Clone()
			cloned = cloned.WithContext(ctx)
			resp, err := c.executeOnce(ctx, cloned, false)
			select {
			case results <- hedgeResult{resp, err}:
			case <-ctx.Done():
			}
		}()
	}

collect:
	// Close the results channel once all goroutines finish.
	go func() {
		wg.Wait()
		close(results)
	}()

	var lastErr error
	var lastResp *Response
	for r := range results {
		if r.err == nil {
			cancel() // Cancel remaining goroutines.
			return r.resp, nil
		}
		lastErr = r.err
		lastResp = r.resp
	}
	return lastResp, lastErr
}
