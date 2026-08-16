package relay

import (
	"context"
	"sync"
	"time"

	"github.com/jhonsferg/relay/internal/pool"
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
	var lastErr error
	var lastResp *Response

	for i := 0; i < maxAttempts; i++ {
		if i > 0 {
			// Wait before launching the next attempt, unless ctx is done.
			timer := pool.GetTimer(after)
			select {
			case <-timer.C:
				pool.PutTimer(timer)
				// Delay elapsed; launch next attempt below.
			case <-ctx.Done():
				pool.PutTimer(timer)
				// No more attempts; wait for results already in flight.
				goto collect
			case r := <-results:
				pool.PutTimer(timer)
				if r.err != nil {
					// A failing attempt arrived while waiting - record it
					// and keep going (fall through to launch the next
					// attempt below), matching the collect loop's own
					// only-succeed-early semantics. Returning here
					// unconditionally would abandon every remaining hedge
					// attempt on the first fast transport error, defeating
					// the point of hedging.
					lastErr = r.err
					lastResp = r.resp
				} else {
					// A successful result arrived while waiting; use it.
					// Cancel remaining attempts and let them unwind in the
					// background - the caller must not pay the latency of
					// waiting for sibling goroutines to notice ctx
					// cancellation before getting the winning response back
					// (this is the common case, since most requests finish
					// before the hedge delay even fires).
					cancel()
					go func() {
						wg.Wait()
						close(results)
						for leftover := range results {
							if leftover.resp != nil && leftover.err == nil {
								PutResponse(leftover.resp)
							}
						}
					}()
					return r.resp, nil
				}
			}
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			// Clone the request so each goroutine has its own copy.
			cloned := req.Clone()
			cloned = cloned.WithContext(ctx)
			resp, err := c.executeOnce(ctx, cloned, false)
			// Return abandoned responses to the pool rather than leaking them.
			select {
			case results <- hedgeResult{resp, err}:
			case <-ctx.Done():
				if resp != nil && err == nil {
					PutResponse(resp)
				}
			}
		}()
	}

collect:
	// Close the results channel once all goroutines finish.
	go func() {
		wg.Wait()
		close(results)
	}()

	for r := range results {
		if r.err == nil {
			cancel() // Cancel remaining goroutines.
			// Drain remaining buffered results; return them to the pool since
			// we already have a winner and nobody else will consume them.
			// The goroutine above will close the channel once goroutines exit.
			go func() {
				for leftover := range results {
					if leftover.resp != nil && leftover.err == nil {
						PutResponse(leftover.resp)
					}
				}
			}()
			return r.resp, nil
		}
		lastErr = r.err
		lastResp = r.resp
	}
	return lastResp, lastErr
}
