package relay

import (
	"context"
	"errors"
	"net"
	"testing"
)

func TestErrorClass_String(t *testing.T) {
	t.Parallel()
	cases := []struct {
		class ErrorClass
		want  string
	}{
		{ErrorClassNone, "none"},
		{ErrorClassTransient, "transient"},
		{ErrorClassPermanent, "permanent"},
		{ErrorClassRateLimited, "rate_limited"},
		{ErrorClassCanceled, "canceled"},
		{ErrorClass(999), "unknown"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := tc.class.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyError_NoError_SuccessResponse(t *testing.T) {
	t.Parallel()
	resp := &Response{StatusCode: 200}
	if got := ClassifyError(nil, resp); got != ErrorClassNone {
		t.Errorf("expected None, got %v", got)
	}
}

func TestClassifyError_NoError_NilResponse(t *testing.T) {
	t.Parallel()
	if got := ClassifyError(nil, nil); got != ErrorClassNone {
		t.Errorf("expected None, got %v", got)
	}
}

func TestClassifyError_NoError_RateLimited(t *testing.T) {
	t.Parallel()
	resp := &Response{StatusCode: 429}
	if got := ClassifyError(nil, resp); got != ErrorClassRateLimited {
		t.Errorf("expected RateLimited, got %v", got)
	}
}

func TestClassifyError_NoError_ServerError(t *testing.T) {
	t.Parallel()
	resp := &Response{StatusCode: 500}
	if got := ClassifyError(nil, resp); got != ErrorClassTransient {
		t.Errorf("expected Transient for 500, got %v", got)
	}
}

func TestClassifyError_NoError_ClientError(t *testing.T) {
	t.Parallel()
	resp := &Response{StatusCode: 404}
	if got := ClassifyError(nil, resp); got != ErrorClassPermanent {
		t.Errorf("expected Permanent for 404, got %v", got)
	}
}

func TestClassifyError_ContextCanceled(t *testing.T) {
	t.Parallel()
	if got := ClassifyError(context.Canceled, nil); got != ErrorClassCanceled {
		t.Errorf("expected Canceled, got %v", got)
	}
}

func TestClassifyError_CircuitOpen(t *testing.T) {
	t.Parallel()
	if got := ClassifyError(ErrCircuitOpen, nil); got != ErrorClassTransient {
		t.Errorf("expected Transient for ErrCircuitOpen, got %v", got)
	}
}

func TestClassifyError_MaxRetriesReached(t *testing.T) {
	t.Parallel()
	if got := ClassifyError(ErrMaxRetriesReached, nil); got != ErrorClassTransient {
		t.Errorf("expected Transient for ErrMaxRetriesReached, got %v", got)
	}
}

func TestClassifyError_Timeout(t *testing.T) {
	t.Parallel()
	if got := ClassifyError(ErrTimeout, nil); got != ErrorClassTransient {
		t.Errorf("expected Transient for ErrTimeout, got %v", got)
	}
}

func TestClassifyError_RateLimitExceeded(t *testing.T) {
	t.Parallel()
	if got := ClassifyError(ErrRateLimitExceeded, nil); got != ErrorClassTransient {
		t.Errorf("expected Transient for ErrRateLimitExceeded, got %v", got)
	}
}

func TestClassifyError_NetworkError(t *testing.T) {
	t.Parallel()
	netErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	if got := ClassifyError(netErr, nil); got != ErrorClassTransient {
		t.Errorf("expected Transient for net.OpError, got %v", got)
	}
}

func TestClassifyError_HTTPError_RateLimited(t *testing.T) {
	t.Parallel()
	httpErr := &HTTPError{StatusCode: 429, Body: []byte("too many")}
	if got := ClassifyError(httpErr, nil); got != ErrorClassRateLimited {
		t.Errorf("expected RateLimited for HTTPError 429, got %v", got)
	}
}

func TestClassifyError_HTTPError_ServerError(t *testing.T) {
	t.Parallel()
	httpErr := &HTTPError{StatusCode: 503, Body: []byte("unavailable")}
	if got := ClassifyError(httpErr, nil); got != ErrorClassTransient {
		t.Errorf("expected Transient for HTTPError 503, got %v", got)
	}
}

func TestClassifyError_HTTPError_ClientError(t *testing.T) {
	t.Parallel()
	httpErr := &HTTPError{StatusCode: 400, Body: []byte("bad request")}
	if got := ClassifyError(httpErr, nil); got != ErrorClassPermanent {
		t.Errorf("expected Permanent for HTTPError 400, got %v", got)
	}
}

func TestClassifyError_Unknown(t *testing.T) {
	t.Parallel()
	if got := ClassifyError(errors.New("some unknown error"), nil); got != ErrorClassTransient {
		t.Errorf("expected Transient for unknown error, got %v", got)
	}
}

func TestIsTransientError(t *testing.T) {
	t.Parallel()
	if !IsTransientError(ErrCircuitOpen, nil) {
		t.Error("ErrCircuitOpen should be transient")
	}
}

func TestIsPermanentError(t *testing.T) {
	t.Parallel()
	resp := &Response{StatusCode: 403}
	if !IsPermanentError(nil, resp) {
		t.Error("403 response should be permanent")
	}
}

func TestIsRateLimitedError(t *testing.T) {
	t.Parallel()
	resp := &Response{StatusCode: 429}
	if !IsRateLimitedError(nil, resp) {
		t.Error("429 response should be rate limited")
	}
}
