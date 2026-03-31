package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
)

// StreamResponse holds a live HTTP response with an unconsumed body reader.
// Unlike [Response], the body is NOT buffered in memory - it is streamed on
// demand. Use this for large payloads (file downloads, SSE, JSONL, etc.).
//
// The caller MUST call Body.Close() when done to release the TCP connection
// back to the pool and free all associated resources. Forgetting to close
// the body causes connection and goroutine leaks.
type StreamResponse struct {
	// StatusCode is the HTTP status code (e.g. 200).
	StatusCode int

	// Status is the human-readable status line (e.g. "200 OK").
	Status string

	// Headers contains all response headers as received from the server.
	Headers http.Header

	// Body is the live, unbuffered response body. The caller must read from it
	// and eventually close it to release the underlying connection.
	Body io.ReadCloser
}

// IsSuccess reports whether the status code is 2xx.
func (s *StreamResponse) IsSuccess() bool { return s.StatusCode >= 200 && s.StatusCode < 300 }

// IsError reports whether the status code is 4xx or 5xx.
func (s *StreamResponse) IsError() bool { return s.StatusCode >= 400 }

// IsClientError reports whether the status code is 4xx.
func (s *StreamResponse) IsClientError() bool { return s.StatusCode >= 400 && s.StatusCode < 500 }

// IsServerError reports whether the status code is 5xx.
func (s *StreamResponse) IsServerError() bool { return s.StatusCode >= 500 }

// Header returns the value of the named response header.
func (s *StreamResponse) Header(key string) string { return s.Headers.Get(key) }

// ContentType returns the Content-Type response header value.
func (s *StreamResponse) ContentType() string { return s.Headers.Get("Content-Type") }

// managedReadCloser wraps an [io.ReadCloser] and calls a list of cleanup
// functions exactly once when the body is closed, regardless of how many times
// Close is called. nil entries in cleanups are safely skipped.
type managedReadCloser struct {
	// ReadCloser is the underlying stream being wrapped.
	io.ReadCloser

	// cleanups is the ordered list of functions to call on first Close.
	cleanups []func()

	// once ensures cleanup functions are invoked at most once.
	once sync.Once
}

// Close closes the underlying ReadCloser and then calls each non-nil cleanup
// function in order. Subsequent calls to Close are no-ops.
func (m *managedReadCloser) Close() error {
	var closeErr error
	m.once.Do(func() {
		closeErr = m.ReadCloser.Close()
		for _, fn := range m.cleanups {
			if fn != nil {
				fn()
			}
		}
	})
	return closeErr
}

// ExecuteStream sends the request and returns a [StreamResponse] without
// buffering the body. Retry logic is NOT applied - a streaming response cannot
// be replayed once partially consumed.
//
// The request participates in [Client.Shutdown]'s graceful drain: the
// in-flight counter is released when Body.Close() is called, not when
// ExecuteStream returns.
//
// [Config.OnBeforeRequest] hooks are applied before the request is sent.
// [Config.OnAfterResponse] hooks are NOT called because the body has not yet
// been consumed when ExecuteStream returns.
//
// If a per-request timeout is set via [Request.WithTimeout], the deadline
// context is canceled automatically when Body.Close() is called.
func (c *Client) ExecuteStream(req *Request) (*StreamResponse, error) {
	if req == nil {
		return nil, ErrNilRequest
	}
	if c.closed.Load() {
		return nil, ErrClientClosed
	}

	ctx := req.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Track in-flight count. On the success path, Done is transferred to the
	// body closer so Shutdown waits until the stream is fully consumed or closed.
	c.inFlight.Add(1)
	released := false
	defer func() {
		if !released {
			c.inFlight.Done()
		}
	}()

	// Apply a per-request timeout whose cancel is forwarded to Body.Close().
	var cancelFn context.CancelFunc
	if req.timeout > 0 {
		ctx, cancelFn = context.WithTimeout(ctx, req.timeout)
		req = req.withCtx(ctx)
	}

	abort := func(err error) error {
		if cancelFn != nil {
			cancelFn()
		}
		return err
	}

	for _, hook := range c.config.OnBeforeRequest {
		if hookErr := hook(ctx, req); hookErr != nil {
			return nil, abort(fmt.Errorf("OnBeforeRequest: %w", hookErr))
		}
	}

	if c.rateLimiter != nil {
		if err := c.rateLimiter.Wait(ctx); err != nil {
			return nil, abort(err)
		}
	}

	if c.circuitBreaker != nil && !c.circuitBreaker.Allow() {
		return nil, abort(ErrCircuitOpen)
	}

	httpReq, err := req.build(c.config.BaseURL, c.config.parsedBaseURL)
	if err != nil {
		return nil, abort(err)
	}

	for k, v := range c.config.DefaultHeaders {
		if httpReq.Header.Get(k) == "" {
			httpReq.Header.Set(k, v)
		}
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if c.circuitBreaker != nil {
			c.circuitBreaker.RecordFailure()
		}
		if cancelFn != nil && errors.Is(err, context.DeadlineExceeded) {
			return nil, abort(fmt.Errorf("%w: %w", ErrTimeout, err))
		}
		return nil, abort(err)
	}

	if c.circuitBreaker != nil {
		if resp.StatusCode >= 500 {
			c.circuitBreaker.RecordFailure()
		} else {
			c.circuitBreaker.RecordSuccess()
		}
	}

	// Build the cleanup chain executed by Body.Close(). Order matters: cancel
	// the timeout context first, then release the in-flight slot.
	cleanups := []func(){
		cancelFn, // nil-safe: managedReadCloser skips nil entries
		c.inFlight.Done,
	}

	// Transfer in-flight responsibility to the body closer.
	released = true
	body := &managedReadCloser{ReadCloser: resp.Body, cleanups: cleanups}

	return &StreamResponse{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Headers:    resp.Header,
		Body:       body,
	}, nil
}
