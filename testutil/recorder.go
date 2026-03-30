package testutil

import (
	"bytes"
	"io"
	"net/http"
	"sync"
)

// RecordedHTTPRequest holds the details of a request that was intercepted by
// a [RequestRecorder].
type RecordedHTTPRequest struct {
	Method  string
	URL     string
	Headers http.Header
	// Body contains the raw request body bytes. May be nil for requests with
	// no body (GET, HEAD) or if the body could not be read.
	Body []byte
}

// RequestRecorder intercepts outgoing requests from a relay client without
// actually sending them. It is useful when you only need to assert on what was
// sent, not on the response.
type RequestRecorder struct {
	mu       sync.Mutex
	requests []*RecordedHTTPRequest
}

// NewRequestRecorder creates a new, empty RequestRecorder.
func NewRequestRecorder() *RequestRecorder { return &RequestRecorder{} }

// Middleware returns an http.RoundTripper wrapping function that records
// every outgoing request. Pass the result to relay.WithTransportMiddleware.
func (r *RequestRecorder) Middleware() func(http.RoundTripper) http.RoundTripper {
	return func(next http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			recorded := &RecordedHTTPRequest{
				Method:  req.Method,
				URL:     req.URL.String(),
				Headers: req.Header.Clone(),
			}
			// Read and restore the request body so the downstream transport
			// still receives the full payload.
			if req.Body != nil && req.Body != http.NoBody {
				body, err := io.ReadAll(req.Body)
				req.Body.Close()
				if err == nil {
					recorded.Body = body
					req.Body = io.NopCloser(bytes.NewReader(body))
				}
			}
			r.mu.Lock()
			r.requests = append(r.requests, recorded)
			r.mu.Unlock()
			return next.RoundTrip(req)
		})
	}
}

// Requests returns a snapshot of all intercepted requests.
func (r *RequestRecorder) Requests() []*RecordedHTTPRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*RecordedHTTPRequest, len(r.requests))
	copy(out, r.requests)
	return out
}

// Reset clears all recorded requests.
func (r *RequestRecorder) Reset() {
	r.mu.Lock()
	r.requests = r.requests[:0]
	r.mu.Unlock()
}

// roundTripperFunc is a helper to create an http.RoundTripper from a function.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
