package relay

import (
	"context"
	"errors"
	"net"
)

// ErrorClass categorises an error returned by Execute into actionable groups,
// allowing callers to make branching decisions without inspecting raw error
// types.
type ErrorClass int

const (
	ErrorClassNone        ErrorClass = iota // no error (success or non-error status)
	ErrorClassTransient                     // may succeed on a subsequent attempt
	ErrorClassPermanent                     // will not succeed on retry (4xx)
	ErrorClassRateLimited                   // 429 Too Many Requests
	ErrorClassCanceled                      // caller canceled the context
)

// String returns the human-readable class name.
func (c ErrorClass) String() string {
	switch c {
	case ErrorClassNone:
		return "none"
	case ErrorClassTransient:
		return "transient"
	case ErrorClassPermanent:
		return "permanent"
	case ErrorClassRateLimited:
		return "rate_limited"
	case ErrorClassCanceled:
		return "canceled"
	default:
		return "unknown"
	}
}

// ClassifyError returns the [ErrorClass] for an error (and optional response)
// returned by Execute. It is the primary entry point for error categorisation.
//
//	class := httpclient.ClassifyError(err, resp)
//	switch class {
//	case httpclient.ErrorClassTransient:  // back off and retry upstream
//	case httpclient.ErrorClassRateLimited: // respect Retry-After
//	case httpclient.ErrorClassPermanent:  // return 400 / log and skip
//	case httpclient.ErrorClassCanceled:   // propagate context cancellation
//	}
func ClassifyError(err error, resp *Response) ErrorClass {
	if err == nil {
		if resp == nil || resp.IsSuccess() {
			return ErrorClassNone
		}
		if resp.StatusCode == 429 {
			return ErrorClassRateLimited
		}
		if resp.IsServerError() {
			return ErrorClassTransient
		}
		if resp.IsClientError() {
			return ErrorClassPermanent
		}
	}

	if errors.Is(err, context.Canceled) {
		return ErrorClassCanceled
	}

	// Well-known client-side transient conditions.
	if errors.Is(err, ErrCircuitOpen) ||
		errors.Is(err, ErrMaxRetriesReached) ||
		errors.Is(err, ErrTimeout) ||
		errors.Is(err, ErrRateLimitExceeded) {
		return ErrorClassTransient
	}

	// Network-level errors (connection refused, DNS failure, etc.) are transient.
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return ErrorClassTransient
	}

	// Typed HTTP errors from AsHTTPError / IsHTTPError.
	if httpErr, ok := IsHTTPError(err); ok {
		if httpErr.StatusCode == 429 {
			return ErrorClassRateLimited
		}
		if httpErr.StatusCode >= 500 {
			return ErrorClassTransient
		}
		if httpErr.StatusCode >= 400 {
			return ErrorClassPermanent
		}
	}

	// Default: treat unknown errors as transient.
	return ErrorClassTransient
}

// IsTransientError reports whether the error may succeed on a subsequent call.
func IsTransientError(err error, resp *Response) bool {
	return ClassifyError(err, resp) == ErrorClassTransient
}

// IsPermanentError reports whether the error will not succeed on retry
// (e.g. 400 Bad Request, 401 Unauthorised, 403 Forbidden, 404 Not Found).
func IsPermanentError(err error, resp *Response) bool {
	return ClassifyError(err, resp) == ErrorClassPermanent
}

// IsRateLimitedError reports whether the error is a 429 Too Many Requests.
func IsRateLimitedError(err error, resp *Response) bool {
	return ClassifyError(err, resp) == ErrorClassRateLimited
}
