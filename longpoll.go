package relay

import (
	"context"
	"net/http"
	"time"
)

// LongPollResult is returned by ExecuteLongPoll.
type LongPollResult struct {
	// Modified is true when the server returned a new response (not 304).
	Modified bool
	// Response is the full response when Modified is true. Nil on 304.
	Response *Response
	// ETag is the ETag from the response (for the next poll).
	ETag string
}

// ExecuteLongPoll sends a long-polling request. It sets a long timeout,
// sends If-None-Match with prevETag (if non-empty), and returns
// LongPollResult. A 304 Not Modified is treated as success with Modified=false.
//
// Typical usage:
//
//	result, err := client.ExecuteLongPoll(ctx, client.Get("/resource"), "", 55*time.Second)
//	if err != nil {
//	    return err
//	}
//	if result.Modified {
//	    fmt.Println("New data:", result.Response.String())
//	}
//	// Use result.ETag for the next poll
func (c *Client) ExecuteLongPoll(ctx context.Context, req *Request, prevETag string, timeout time.Duration) (LongPollResult, error) {
	if req == nil {
		return LongPollResult{}, ErrNilRequest
	}

	req = req.WithContext(ctx).WithTimeout(timeout)

	if prevETag != "" {
		req = req.WithHeader("If-None-Match", prevETag)
	}

	resp, err := c.Execute(req)
	if err != nil {
		return LongPollResult{}, err
	}

	if resp.StatusCode == http.StatusNotModified {
		return LongPollResult{
			Modified: false,
			Response: nil,
			ETag:     resp.Header("ETag"),
		}, nil
	}

	return LongPollResult{
		Modified: true,
		Response: resp,
		ETag:     resp.Header("ETag"),
	}, nil
}
