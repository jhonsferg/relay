package relay

import (
	"errors"
	"fmt"
	"testing"
)

func TestHTTPError_Error(t *testing.T) {
	t.Parallel()
	e := &HTTPError{StatusCode: 404, Status: "404 Not Found", Body: []byte("not found")}
	got := e.Error()
	if got != "http error: status=404 body=not found" {
		t.Errorf("unexpected error string: %q", got)
	}
}

func TestIsHTTPError_Found(t *testing.T) {
	t.Parallel()
	httpErr := &HTTPError{StatusCode: 500, Body: []byte("internal")}
	wrapped := fmt.Errorf("wrapped: %w", httpErr)
	got, ok := IsHTTPError(wrapped)
	if !ok {
		t.Fatal("expected IsHTTPError to return true")
	}
	if got.StatusCode != 500 {
		t.Errorf("expected status 500, got %d", got.StatusCode)
	}
}

func TestIsHTTPError_NotFound(t *testing.T) {
	t.Parallel()
	_, ok := IsHTTPError(errors.New("plain error"))
	if ok {
		t.Error("expected IsHTTPError to return false for plain error")
	}
}

func TestIsHTTPError_Nil(t *testing.T) {
	t.Parallel()
	_, ok := IsHTTPError(nil)
	if ok {
		t.Error("expected IsHTTPError to return false for nil")
	}
}

func TestSentinelErrors_Distinct(t *testing.T) {
	t.Parallel()
	sentinels := []error{
		ErrCircuitOpen,
		ErrMaxRetriesReached,
		ErrRateLimitExceeded,
		ErrNilRequest,
		ErrTimeout,
		ErrBodyTruncated,
		ErrClientClosed,
	}
	for i, a := range sentinels {
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Errorf("sentinel errors[%d] and [%d] are equal: %v == %v", i, j, a, b)
			}
		}
	}
}
