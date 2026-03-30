package relay

// AsyncResult holds the outcome of an asynchronous HTTP request.
type AsyncResult struct {
	Response *Response
	Err      error
}

// ExecuteAsync sends the request in a background goroutine and returns a
// buffered channel that receives exactly one [AsyncResult] when the request
// completes. The channel is closed after delivery.
//
// Use a select with a context-aware case to impose an external deadline:
//
//	ch := client.ExecuteAsync(req)
//	select {
//	case result := <-ch:
//	    if result.Err != nil { ... }
//	    fmt.Println(result.Response.StatusCode)
//	case <-ctx.Done():
//	    // The request continues in the background; its own context governs it.
//	}
func (c *Client) ExecuteAsync(req *Request) <-chan AsyncResult {
	ch := make(chan AsyncResult, 1)
	go func() {
		resp, err := c.Execute(req)
		ch <- AsyncResult{Response: resp, Err: err}
		close(ch)
	}()
	return ch
}

// ExecuteAsyncCallback sends the request in a background goroutine and invokes
// the appropriate callback on completion. Either callback may be nil.
//
//	client.ExecuteAsyncCallback(req,
//	    func(resp *Response) { log.Println("ok", resp.StatusCode) },
//	    func(err error)      { log.Println("failed", err) },
//	)
func (c *Client) ExecuteAsyncCallback(req *Request, onSuccess func(*Response), onError func(error)) {
	go func() {
		resp, err := c.Execute(req)
		if err != nil {
			if onError != nil {
				onError(err)
			}
			return
		}
		if onSuccess != nil {
			onSuccess(resp)
		}
	}()
}
