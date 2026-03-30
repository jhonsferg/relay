// Package testutil provides test helpers for code that uses the relay HTTP
// client. It is inspired by OkHttp's MockWebServer.
package testutil

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

// MockResponse defines the response the MockServer should return for the next
// queued request.
type MockResponse struct {
	// Status is the HTTP status code (default 200).
	Status int

	// Headers are response headers merged into the reply.
	Headers map[string]string

	// Body is the response body as a string.
	Body string

	// Delay introduces artificial latency before the response is written.
	Delay time.Duration

	// isError causes the server to abruptly close the connection instead of
	// writing a response (simulates a network error).
	isError bool
}

// RecordedRequest holds details of an HTTP request captured by the MockServer.
type RecordedRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    []byte
	Query   url.Values
}

// MockServer is a test HTTP server that serves queued responses in FIFO order
// and records incoming requests.
type MockServer struct {
	server   *httptest.Server
	mu       sync.Mutex
	queue    []MockResponse
	recorded []RecordedRequest
	count    atomic.Int64
	newReqCh chan struct{}
}

// NewMockServer starts a new local HTTP test server. Call [MockServer.Close]
// when the test is done.
func NewMockServer() *MockServer {
	ms := &MockServer{
		newReqCh: make(chan struct{}, 128),
	}
	ms.server = httptest.NewServer(http.HandlerFunc(ms.handle))
	return ms
}

func (s *MockServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	var resp MockResponse
	if len(s.queue) > 0 {
		resp = s.queue[0]
		s.queue = s.queue[1:]
	} else {
		// Default: 200 OK with empty body.
		resp = MockResponse{Status: http.StatusOK}
	}
	s.mu.Unlock()

	// Record the request.
	body, _ := io.ReadAll(r.Body)
	recorded := RecordedRequest{
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: r.Header.Clone(),
		Body:    body,
		Query:   r.URL.Query(),
	}
	s.mu.Lock()
	s.recorded = append(s.recorded, recorded)
	s.mu.Unlock()
	s.count.Add(1)

	select {
	case s.newReqCh <- struct{}{}:
	default:
	}

	if resp.isError {
		// Abruptly close the connection to simulate a network error.
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			if conn != nil {
				_ = conn.Close() //nolint:errcheck
			}
		}
		return
	}

	if resp.Delay > 0 {
		time.Sleep(resp.Delay)
	}

	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}

	for k, v := range resp.Headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(status)
	if resp.Body != "" {
		w.Write([]byte(resp.Body)) //nolint:errcheck
	}
}

// Enqueue adds one or more responses to the response queue. Responses are
// served in FIFO order.
func (s *MockServer) Enqueue(responses ...MockResponse) {
	s.mu.Lock()
	s.queue = append(s.queue, responses...)
	s.mu.Unlock()
}

// EnqueueError causes the next request to fail with a connection error.
func (s *MockServer) EnqueueError() {
	s.mu.Lock()
	s.queue = append(s.queue, MockResponse{isError: true})
	s.mu.Unlock()
}

// URL returns the base URL of the test server (e.g. "http://127.0.0.1:PORT").
func (s *MockServer) URL() string { return s.server.URL }

// Close shuts down the test server.
func (s *MockServer) Close() { s.server.Close() }

// TakeRequest returns the next recorded request in FIFO order. It blocks until
// a request arrives or the timeout elapses, whichever comes first.
// Returns an error if the timeout expires before a request is available.
func (s *MockServer) TakeRequest(timeout time.Duration) (*RecordedRequest, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	for {
		s.mu.Lock()
		if len(s.recorded) > 0 {
			req := s.recorded[0]
			s.recorded = s.recorded[1:]
			s.mu.Unlock()
			return &req, nil
		}
		s.mu.Unlock()

		select {
		case <-s.newReqCh:
			// Try again.
		case <-deadline.C:
			return nil, errors.New("testutil: TakeRequest timed out")
		}
	}
}

// RequestCount returns the total number of requests received since the server
// was created.
func (s *MockServer) RequestCount() int { return int(s.count.Load()) }
