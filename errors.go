package relay

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by Execute, ExecuteStream, and related methods.
var (
	// ErrCircuitOpen is returned when the circuit breaker is in the Open state
	// and the request is rejected without being sent.
	ErrCircuitOpen = errors.New("circuit breaker is open")

	// ErrMaxRetriesReached is returned when all retry attempts have been
	// exhausted and the last attempt ended with a retryable error.
	ErrMaxRetriesReached = errors.New("max retries reached")

	// ErrRateLimitExceeded is returned when the rate limiter cannot grant a
	// token before the request context expires.
	ErrRateLimitExceeded = errors.New("rate limit exceeded")

	// ErrNilRequest is returned when a nil *Request is passed to Execute.
	ErrNilRequest = errors.New("request cannot be nil")

	// ErrTimeout wraps context.DeadlineExceeded when the per-request timeout
	// set via WithTimeout fires.
	ErrTimeout = errors.New("request timed out")

	// ErrBodyTruncated is a sentinel that callers may check against when they
	// need to detect truncation programmatically; the actual truncation is
	// signalled by Response.IsTruncated().
	ErrBodyTruncated = errors.New("response body exceeded size limit and was truncated")

	// ErrClientClosed is returned when Execute is called after Shutdown.
	ErrClientClosed = errors.New("client is closed")
)

// HTTPError represents an HTTP response whose status code indicates a client
// or server error (4xx or 5xx). It is returned by Response.AsHTTPError and
// can be used with errors.As.
type HTTPError struct {
	StatusCode int
	Status     string
	Body       []byte
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http error: status=%d body=%s", e.StatusCode, string(e.Body))
}

// IsHTTPError reports whether err (or any error in its chain) is an *HTTPError
// and returns it if so. Use this instead of errors.As when you want both the
// bool and the typed value in one call.
func IsHTTPError(err error) (*HTTPError, bool) {
	var httpErr *HTTPError
	ok := errors.As(err, &httpErr)
	return httpErr, ok
}
