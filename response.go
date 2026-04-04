package relay

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/jhonsferg/relay/internal/pool"
)

// Response is a fully buffered HTTP response. The body has been read and
// closed by the time Response is returned from Execute. Use ExecuteStream
// for payloads that should not be buffered entirely in memory.
type Response struct {
	raw           *http.Response
	body          []byte
	decode        func(contentType string, body []byte, v any) error
	StatusCode    int
	Status        string
	Headers       http.Header
	Truncated     bool          // true when body was cut at MaxResponseBodyBytes
	RedirectCount int           // number of redirects followed to reach this response
	Timing        RequestTiming // per-phase timing breakdown (DNS, TCP, TLS, TTFB, ...)
}

var responsePool = &sync.Pool{
	New: func() any { return new(Response) },
}

// getResponse returns a pooled Response, cleared for reuse.
func getResponse() *Response {
	r := responsePool.Get().(*Response)
	r.reset()
	return r
}

// reset clears all fields except body slice (capacity kept for potential reuse).
func (r *Response) reset() {
	r.raw = nil
	r.body = r.body[:0]
	r.decode = nil
	r.StatusCode = 0
	r.Status = ""
	r.Headers = nil
	r.Truncated = false
	r.RedirectCount = 0
	r.Timing = RequestTiming{}
}

// PutResponse returns a Response to the pool. The Response must not be used
// after calling this function. Callers should call this when they are done
// with the response and not retaining it.
func PutResponse(r *Response) {
	if r != nil {
		responsePool.Put(r)
	}
}

func newResponse(resp *http.Response, maxBytes int64, redirectCount int) (*Response, error) {
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck

	var reader io.Reader = resp.Body
	if maxBytes > 0 {
		reader = io.LimitReader(resp.Body, maxBytes+1)
	}

	// Use a sized pooled buffer to reduce GC pressure.
	// Size selection based on Content-Length hint for optimal tier reuse.
	poolBuf := pool.GetSizedBuffer(resp.ContentLength)
	buf := bytes.NewBuffer(*poolBuf)
	buf.Reset()

	_, err := buf.ReadFrom(reader)
	if err != nil {
		pool.PutSizedBuffer(poolBuf)
		return nil, err
	}

	body := buf.Bytes()
	truncated := false
	if maxBytes > 0 && int64(len(body)) > maxBytes {
		// Trim to the limit and copy to a right-sized slice.
		body = append([]byte(nil), body[:maxBytes]...)
		truncated = true
	} else if len(body) > 0 {
		// Copy to a right-sized slice so the large buffer can be GC'd.
		body = append([]byte(nil), body...)
	} else {
		// Empty body: no allocation needed
		body = nil
	}

	// Return the pool buffer now that body is safely copied.
	pool.PutSizedBuffer(poolBuf)

	// Get pooled response struct.
	r := getResponse()
	r.raw = resp
	r.body = body
	r.StatusCode = resp.StatusCode
	r.Status = resp.Status
	r.Headers = resp.Header
	r.Truncated = truncated
	r.RedirectCount = redirectCount

	return r, nil
}

// Body returns the full response body as a byte slice. The slice is owned by
// Response; callers must not modify it.
func (r *Response) Body() []byte { return r.body }

// String returns the response body decoded as a UTF-8 string.
func (r *Response) String() string { return string(r.body) }

// BodyReader returns a new [io.Reader] positioned at the start of the buffered
// body. Each call returns an independent reader; the underlying bytes are shared.
func (r *Response) BodyReader() io.Reader { return bytes.NewReader(r.body) }

// IsTruncated reports whether the body was cut at MaxResponseBodyBytes.
func (r *Response) IsTruncated() bool { return r.Truncated }

// WasRedirected reports whether at least one redirect was followed.
func (r *Response) WasRedirected() bool { return r.RedirectCount > 0 }

// ContentType returns the Content-Type response header value.
func (r *Response) ContentType() string { return r.Headers.Get("Content-Type") }

// Header returns the value of the named response header.
func (r *Response) Header(key string) string { return r.Headers.Get(key) }

// Location returns the value of the Location response header, or "" if absent.
// Useful when inspecting redirects with redirect-following disabled.
func (r *Response) Location() string { return r.Headers.Get("Location") }

// Cookies parses and returns all cookies set by the server via Set-Cookie
// headers.
func (r *Response) Cookies() []*http.Cookie { return r.raw.Cookies() }

// Raw returns the underlying *http.Response. The response body has already
// been consumed; use Body, String, or JSON to access the buffered bytes.
func (r *Response) Raw() *http.Response { return r.raw }

// JSON unmarshals the response body into v using encoding/json.
func (r *Response) JSON(v interface{}) error { return json.Unmarshal(r.body, v) }

// XML unmarshals the response body into v using encoding/xml.
func (r *Response) XML(v interface{}) error { return xml.Unmarshal(r.body, v) }

// Decode deserialises the response body into v. When a [WithResponseDecoder]
// has been configured on the client, it is called with the response
// Content-Type header and the body bytes. Otherwise Decode falls back to
// JSON for application/json content and XML for application/xml content.
//
// Decode is used internally by [ExecuteAs] to allow pluggable decoders
// (e.g. Protocol Buffers, MessagePack) without changing call sites.
func (r *Response) Decode(v any) error {
	if r.decode != nil {
		return r.decode(r.ContentType(), r.body, v)
	}
	ct := r.ContentType()
	if strings.Contains(ct, "xml") {
		return xml.Unmarshal(r.body, v)
	}
	return json.Unmarshal(r.body, v)
}

// IsSuccess reports whether the status code is 2xx.
func (r *Response) IsSuccess() bool { return r.StatusCode >= 200 && r.StatusCode < 300 }

// IsRedirect reports whether the status code is 3xx.
func (r *Response) IsRedirect() bool { return r.StatusCode >= 300 && r.StatusCode < 400 }

// IsClientError reports whether the status code is 4xx.
func (r *Response) IsClientError() bool { return r.StatusCode >= 400 && r.StatusCode < 500 }

// IsServerError reports whether the status code is 5xx.
func (r *Response) IsServerError() bool { return r.StatusCode >= 500 }

// IsError reports whether the status code is 4xx or 5xx.
func (r *Response) IsError() bool { return r.StatusCode >= 400 }

// AsHTTPError returns an *HTTPError for 4xx/5xx responses, or nil for success.
// Use this to convert an HTTP error status into a Go error.
func (r *Response) AsHTTPError() *HTTPError {
	if !r.IsError() {
		return nil
	}
	return &HTTPError{StatusCode: r.StatusCode, Status: r.Status, Body: r.body}
}
