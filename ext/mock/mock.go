// Package mock provides a programmable HTTP transport for testing relay clients
// without making real network calls.
//
// Usage:
//
//	import (
//	    "github.com/jhonsferg/relay"
//	    relaymock "github.com/jhonsferg/relay/ext/mock"
//	)
//
//	mt := relaymock.New()
//	mt.On(relaymock.MatchMethod("GET"), relaymock.MatchPath("/users/1")).
//	    RespondJSON(200, map[string]any{"id": 1, "name": "Alice"})
//
//	client := relay.New(relaymock.WithMock(mt))
//
//	resp, err := client.Execute(client.Get("/users/1"))
//	// resp.StatusCode == 200
//
//	mt.AssertCallCount(t, 1)
//
// # Rule matching
//
// Rules are evaluated in registration order. The first rule whose matchers all
// return true handles the request. If no rule matches, the transport returns an
// [ErrNoMatchingRule] error (or forwards to the optional fallback transport).
//
// # Sequences
//
// [Rule.RespondSequence] lets a single rule return different responses on
// consecutive calls - useful for testing retry logic:
//
//	mt.On(relaymock.MatchAny()).
//	    RespondSequence(
//	        relaymock.Seq(500, nil, nil),       // first call → 500
//	        relaymock.Seq(500, nil, nil),       // second call → 500
//	        relaymock.Seq(200, body, nil),      // third call → success
//	    )
package mock

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/jhonsferg/relay"
)

// ErrNoMatchingRule is returned when a request does not match any registered rule
// and no fallback transport is configured.
var ErrNoMatchingRule = errors.New("mock: no matching rule for request")

// Matcher is a predicate applied to an incoming HTTP request.
type Matcher func(req *http.Request) bool

// -- Matchers ------------------------------------------------------------------

// MatchAny matches every request - use as a catch-all rule.
func MatchAny() Matcher { return func(*http.Request) bool { return true } }

// MatchMethod matches requests with the given HTTP method (case-insensitive).
func MatchMethod(method string) Matcher {
	m := strings.ToUpper(method)
	return func(req *http.Request) bool { return strings.ToUpper(req.Method) == m }
}

// MatchURL matches requests whose full URL equals url exactly.
func MatchURL(url string) Matcher {
	return func(req *http.Request) bool { return req.URL.String() == url }
}

// MatchPath matches requests whose URL path equals path exactly.
func MatchPath(path string) Matcher {
	return func(req *http.Request) bool { return req.URL.Path == path }
}

// MatchPathPrefix matches requests whose URL path starts with prefix.
func MatchPathPrefix(prefix string) Matcher {
	return func(req *http.Request) bool { return strings.HasPrefix(req.URL.Path, prefix) }
}

// MatchHeader matches requests that contain a header with the given key and value.
func MatchHeader(key, value string) Matcher {
	return func(req *http.Request) bool { return req.Header.Get(key) == value }
}

// MatchQueryParam matches requests whose URL contains the given query parameter value.
func MatchQueryParam(key, value string) Matcher {
	return func(req *http.Request) bool { return req.URL.Query().Get(key) == value }
}

// -- Recorded call -------------------------------------------------------------

// Call records a single intercepted request and its outcome.
type Call struct {
	// Request is the intercepted *http.Request.
	Request *http.Request
	// Response is the *http.Response returned to the caller. Nil when Err is set.
	Response *http.Response
	// Err is the error returned to the caller.
	Err error
}

// -- Sequence entry ------------------------------------------------------------

// SeqEntry is one step in a [Rule.RespondSequence] list.
type SeqEntry struct {
	statusCode int
	body       []byte
	headers    map[string]string
	err        error
}

// Seq creates a sequence entry: an HTTP response with the given status, body,
// and optional headers. Pass nil body for an empty response body.
func Seq(statusCode int, body []byte, headers map[string]string) *SeqEntry {
	return &SeqEntry{statusCode: statusCode, body: body, headers: headers}
}

// SeqError creates a sequence entry that returns a transport error instead of
// an HTTP response - simulating network failures for retry testing.
func SeqError(err error) *SeqEntry {
	return &SeqEntry{err: err}
}

// -- Rule ----------------------------------------------------------------------

// Rule maps a set of request matchers to a response producer.
// Obtain a Rule via [MockTransport.On]; configure it using its fluent methods.
type Rule struct {
	mt       *MockTransport
	matchers []Matcher
	mu       sync.Mutex

	// static response (set by Respond / RespondJSON / RespondError)
	staticStatus  int
	staticBody    []byte
	staticHeaders map[string]string
	staticErr     error

	// sequence (set by RespondSequence)
	sequence []*SeqEntry
	seqIdx   int

	useSequence bool
}

func (r *Rule) matches(req *http.Request) bool {
	for _, m := range r.matchers {
		if !m(req) {
			return false
		}
	}
	return true
}

func (r *Rule) respond(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.useSequence {
		if len(r.sequence) == 0 {
			return nil, errors.New("mock: RespondSequence called with no entries")
		}
		idx := r.seqIdx
		if idx >= len(r.sequence) {
			idx = len(r.sequence) - 1 // repeat last entry
		}
		r.seqIdx++
		entry := r.sequence[idx]
		if entry.err != nil {
			return nil, entry.err
		}
		return buildResponse(req, entry.statusCode, entry.body, entry.headers), nil
	}

	if r.staticErr != nil {
		return nil, r.staticErr
	}
	return buildResponse(req, r.staticStatus, r.staticBody, r.staticHeaders), nil
}

// Respond configures the rule to return an HTTP response with the given status
// code, body, and optional headers.
func (r *Rule) Respond(statusCode int, body []byte, headers ...map[string]string) *Rule {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.staticStatus = statusCode
	r.staticBody = body
	if len(headers) > 0 {
		r.staticHeaders = headers[0]
	}
	return r
}

// RespondJSON configures the rule to return a JSON-encoded body. The
// Content-Type header is set to application/json automatically.
func (r *Rule) RespondJSON(statusCode int, v any) *Rule {
	body, err := json.Marshal(v)
	if err != nil {
		body = []byte(`{"error":"marshal error"}`)
	}
	return r.Respond(statusCode, body, map[string]string{"Content-Type": "application/json"})
}

// RespondError configures the rule to return a transport-level error instead
// of an HTTP response. This simulates network failures, timeouts, etc.
func (r *Rule) RespondError(err error) *Rule {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.staticErr = err
	return r
}

// RespondSequence configures the rule to return different responses on
// consecutive calls. After exhausting the list the last entry is repeated.
func (r *Rule) RespondSequence(entries ...*SeqEntry) *Rule {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sequence = entries
	r.useSequence = true
	return r
}

// -- MockTransport -------------------------------------------------------------

// MockTransport is a programmable [http.RoundTripper] for testing.
// All methods are safe for concurrent use.
type MockTransport struct {
	mu       sync.RWMutex
	rules    []*Rule
	calls    []*Call
	fallback http.RoundTripper
}

// New creates a new MockTransport with no rules configured.
func New() *MockTransport { return &MockTransport{} }

// WithFallback sets a transport to use when no rule matches. By default,
// unmatched requests return [ErrNoMatchingRule].
func (mt *MockTransport) WithFallback(rt http.RoundTripper) *MockTransport {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	mt.fallback = rt
	return mt
}

// On registers a new rule that fires when all provided matchers return true.
// Rules are evaluated in registration order; the first match wins.
func (mt *MockTransport) On(matchers ...Matcher) *Rule {
	r := &Rule{mt: mt, matchers: matchers}
	mt.mu.Lock()
	mt.rules = append(mt.rules, r)
	mt.mu.Unlock()
	return r
}

// RoundTrip implements [http.RoundTripper]. It finds the first matching rule
// and returns its configured response, recording the call.
func (mt *MockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	mt.mu.RLock()
	rules := mt.rules
	fallback := mt.fallback
	mt.mu.RUnlock()

	for _, r := range rules {
		if r.matches(req) {
			resp, err := r.respond(req)
			mt.record(req, resp, err)
			return resp, err
		}
	}

	if fallback != nil {
		return fallback.RoundTrip(req)
	}
	err := fmt.Errorf("%w: %s %s", ErrNoMatchingRule, req.Method, req.URL)
	mt.record(req, nil, err)
	return nil, err
}

func (mt *MockTransport) record(req *http.Request, resp *http.Response, err error) {
	mt.mu.Lock()
	mt.calls = append(mt.calls, &Call{Request: req, Response: resp, Err: err})
	mt.mu.Unlock()
}

// Calls returns a copy of all recorded calls in chronological order.
func (mt *MockTransport) Calls() []*Call {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	out := make([]*Call, len(mt.calls))
	copy(out, mt.calls)
	return out
}

// CallCount returns the total number of requests intercepted so far.
func (mt *MockTransport) CallCount() int {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return len(mt.calls)
}

// Reset clears all recorded calls and removes all registered rules.
func (mt *MockTransport) Reset() {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	mt.rules = nil
	mt.calls = nil
}

// AssertCallCount fails t if the total number of intercepted requests does not
// equal n.
func (mt *MockTransport) AssertCallCount(t TB, n int) bool {
	t.Helper()
	if got := mt.CallCount(); got != n {
		t.Errorf("mock: expected %d call(s), got %d", n, got)
		return false
	}
	return true
}

// AssertCalled fails t if no intercepted call satisfies all provided matchers.
func (mt *MockTransport) AssertCalled(t TB, matchers ...Matcher) bool {
	t.Helper()
	for _, c := range mt.Calls() {
		allMatch := true
		for _, m := range matchers {
			if !m(c.Request) {
				allMatch = false
				break
			}
		}
		if allMatch {
			return true
		}
	}
	t.Errorf("mock: AssertCalled - no call matched all matchers")
	return false
}

// AssertNotCalled fails t if any intercepted call satisfies all provided matchers.
func (mt *MockTransport) AssertNotCalled(t TB, matchers ...Matcher) bool {
	t.Helper()
	for _, c := range mt.Calls() {
		allMatch := true
		for _, m := range matchers {
			if !m(c.Request) {
				allMatch = false
				break
			}
		}
		if allMatch {
			t.Errorf("mock: AssertNotCalled - a call matched all matchers")
			return false
		}
	}
	return true
}

// TB is the subset of [testing.TB] used by assertion methods. It is satisfied
// by *testing.T, *testing.B, and *testing.F.
type TB interface {
	Helper()
	Errorf(format string, args ...any)
}

// -- WithMock option -----------------------------------------------------------

// WithMock returns a [relay.Option] that replaces the relay transport stack
// with mt. All requests are intercepted by the mock; no real network calls
// are made unless a fallback is configured via [MockTransport.WithFallback].
func WithMock(mt *MockTransport) relay.Option {
	return relay.WithTransportMiddleware(func(_ http.RoundTripper) http.RoundTripper {
		return mt
	})
}

// -- helpers -------------------------------------------------------------------

func buildResponse(req *http.Request, statusCode int, body []byte, headers map[string]string) *http.Response {
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	resp := &http.Response{
		StatusCode: statusCode,
		Status:     fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
		Header:     make(http.Header),
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Request:    req,
	}
	for k, v := range headers {
		resp.Header.Set(k, v)
	}
	if body != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		resp.ContentLength = int64(len(body))
	} else {
		resp.Body = io.NopCloser(strings.NewReader(""))
		resp.ContentLength = 0
	}
	return resp
}
