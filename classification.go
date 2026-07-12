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
		return classifyFromResponse(resp)
	}
	return classifyFromError(err)
}

// classifyFromResponse handles the err == nil path: classification based
// purely on the HTTP response status.
func classifyFromResponse(resp *Response) ErrorClass {
	if resp == nil || resp.IsSuccess() {
		return ErrorClassNone
	}
	switch {
	case resp.StatusCode == 429:
		return ErrorClassRateLimited
	case resp.IsServerError():
		return ErrorClassTransient
	case resp.IsClientError():
		return ErrorClassPermanent
	default:
		// Non-success status outside 4xx/5xx (e.g. an unexpected 3xx that
		// slipped through) - matches the original fallthrough behaviour of
		// treating unclassified conditions as transient.
		return ErrorClassTransient
	}
}

// classifyFromError handles the err != nil path: classification based on
// well-known sentinel errors, network errors, and typed HTTP errors.
func classifyFromError(err error) ErrorClass {
	if errors.Is(err, context.Canceled) {
		return ErrorClassCanceled
	}

	if isKnownTransientError(err) {
		return ErrorClassTransient
	}

	// Client-level permanent conditions: retrying will not help.
	if errors.Is(err, ErrClientClosed) ||
		errors.Is(err, ErrCertificatePinMismatch) ||
		errors.Is(err, ErrNilRequest) {
		return ErrorClassPermanent
	}

	// Network-level errors (connection refused, DNS failure, etc.) are transient.
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return ErrorClassTransient
	}

	// Typed HTTP errors from AsHTTPError / IsHTTPError.
	if class, ok := classifyHTTPError(err); ok {
		return class
	}

	// Default: treat unknown errors as transient.
	return ErrorClassTransient
}

// isKnownTransientError reports whether err is one of the well-known
// client-side sentinel errors that are always safe to retry.
func isKnownTransientError(err error) bool {
	return errors.Is(err, ErrCircuitOpen) ||
		errors.Is(err, ErrMaxRetriesReached) ||
		errors.Is(err, ErrTimeout) ||
		errors.Is(err, ErrRateLimitExceeded) ||
		errors.Is(err, ErrBulkheadFull) ||
		errors.Is(err, ErrRetryBudgetExhausted)
}

// classifyHTTPError classifies a typed HTTP error (from AsHTTPError /
// IsHTTPError) by its status code. ok is false if err is not an HTTPError.
func classifyHTTPError(err error) (class ErrorClass, ok bool) {
	httpErr, isHTTPErr := IsHTTPError(err)
	if !isHTTPErr {
		return ErrorClassNone, false
	}
	switch {
	case httpErr.StatusCode == 429:
		return ErrorClassRateLimited, true
	case httpErr.StatusCode >= 500:
		return ErrorClassTransient, true
	case httpErr.StatusCode >= 400:
		return ErrorClassPermanent, true
	default:
		return ErrorClassNone, false
	}
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

// IsRetryableError reports whether the error is worth retrying - that is,
// whether [ClassifyError] returns [ErrorClassTransient] or
// [ErrorClassRateLimited]. It is a convenience shorthand for the common
// "should I back off and try again?" decision.
func IsRetryableError(err error, resp *Response) bool {
	class := ClassifyError(err, resp)
	return class == ErrorClassTransient || class == ErrorClassRateLimited
}

// IsTimeout reports whether the error represents a request timeout. It matches
// both [ErrTimeout] (returned when a per-request timeout fires) and the
// standard [context.DeadlineExceeded] sentinel.
func IsTimeout(err error) bool {
	return errors.Is(err, ErrTimeout) || errors.Is(err, context.DeadlineExceeded)
}

// IsCircuitOpen reports whether the error was caused by the circuit breaker
// being in the Open state (i.e. [ErrCircuitOpen]).
func IsCircuitOpen(err error) bool {
	return errors.Is(err, ErrCircuitOpen)
}
