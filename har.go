package relay

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// HAREntry represents a single request/response pair in HAR 1.2 format.
type HAREntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            float64     `json:"time"`
	Request         HARRequest  `json:"request"`
	Response        HARResponse `json:"response"`
	Timings         HARTimings  `json:"timings"`
}

// HARRequest is the HAR 1.2 request object.
type HARRequest struct {
	Method      string       `json:"method"`
	URL         string       `json:"url"`
	HTTPVersion string       `json:"httpVersion"`
	Headers     []HARNameVal `json:"headers"`
	QueryString []HARNameVal `json:"queryString"`
	PostData    *HARPostData `json:"postData,omitempty"`
	BodySize    int          `json:"bodySize"`
	HeadersSize int          `json:"headersSize"`
}

// HARResponse is the HAR 1.2 response object.
type HARResponse struct {
	Status      int          `json:"status"`
	StatusText  string       `json:"statusText"`
	HTTPVersion string       `json:"httpVersion"`
	Headers     []HARNameVal `json:"headers"`
	Content     HARContent   `json:"content"`
	RedirectURL string       `json:"redirectURL"`
	BodySize    int          `json:"bodySize"`
	HeadersSize int          `json:"headersSize"`
}

// HARTimings holds HAR 1.2 timing breakdown.
type HARTimings struct {
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
}

// HARNameVal is a key/value pair used for headers and query params.
type HARNameVal struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HARContent holds response body information.
type HARContent struct {
	Size     int    `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
}

// HARPostData holds request body information.
type HARPostData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

// harLog is the top-level HAR 1.2 log object.
type harLog struct {
	Version string     `json:"version"`
	Creator harCreator `json:"creator"`
	Entries []HAREntry `json:"entries"`
}

type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// HARRecorder captures HTTP request/response pairs in HAR 1.2 format.
// It is safe for concurrent use.
type HARRecorder struct {
	mu      sync.Mutex
	entries []HAREntry
}

// NewHARRecorder creates a new, empty HARRecorder.
func NewHARRecorder() *HARRecorder {
	return &HARRecorder{}
}

// record adds a captured entry to the recorder.
func (r *HARRecorder) record(entry HAREntry) {
	r.mu.Lock()
	r.entries = append(r.entries, entry)
	r.mu.Unlock()
}

// Entries returns a snapshot of all recorded entries.
func (r *HARRecorder) Entries() []HAREntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]HAREntry, len(r.entries))
	copy(out, r.entries)
	return out
}

// Export serialises all recorded entries as a HAR 1.2 JSON document.
func (r *HARRecorder) Export() ([]byte, error) {
	r.mu.Lock()
	entries := make([]HAREntry, len(r.entries))
	copy(entries, r.entries)
	r.mu.Unlock()

	doc := map[string]any{
		"log": harLog{
			Version: "1.2",
			Creator: harCreator{Name: "relay", Version: "0.1.0"},
			Entries: entries,
		},
	}
	return json.MarshalIndent(doc, "", "  ")
}

// Reset clears all recorded entries.
func (r *HARRecorder) Reset() {
	r.mu.Lock()
	r.entries = r.entries[:0]
	r.mu.Unlock()
}

// harTransport is an http.RoundTripper that records request/response pairs.
type harTransport struct {
	base     http.RoundTripper
	recorder *HARRecorder
}

func newHARTransport(base http.RoundTripper, rec *HARRecorder) http.RoundTripper {
	return &harTransport{base: base, recorder: rec}
}

func (t *harTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()

	// Capture request details.
	harReq := buildHARRequest(req)

	resp, err := t.base.RoundTrip(req)
	elapsed := time.Since(start)

	if err != nil {
		return nil, err
	}

	// Read and restore response body for recording.
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close() //nolint:errcheck
	if readErr != nil {
		return nil, fmt.Errorf("relay: har recording: %w", readErr)
	}
	resp.Body = io.NopCloser(newBytesReader(body))

	harResp := buildHARResponse(resp, body)

	entry := HAREntry{
		StartedDateTime: start.UTC().Format(time.RFC3339Nano),
		Time:            float64(elapsed.Milliseconds()),
		Request:         harReq,
		Response:        harResp,
		Timings: HARTimings{
			Send:    0,
			Wait:    float64(elapsed.Milliseconds()),
			Receive: 0,
		},
	}
	t.recorder.record(entry)

	return resp, nil
}

func buildHARRequest(req *http.Request) HARRequest {
	var headers []HARNameVal
	for k, vs := range req.Header {
		for _, v := range vs {
			headers = append(headers, HARNameVal{Name: k, Value: v})
		}
	}

	var queryParams []HARNameVal
	for k, vs := range req.URL.Query() {
		for _, v := range vs {
			queryParams = append(queryParams, HARNameVal{Name: k, Value: v})
		}
	}

	harReq := HARRequest{
		Method:      req.Method,
		URL:         req.URL.String(),
		HTTPVersion: "HTTP/1.1",
		Headers:     headers,
		QueryString: queryParams,
		HeadersSize: -1,
		BodySize:    -1,
	}

	// Capture the request body: read it, record it, and restore it so the
	// actual transport still gets the full payload.
	if req.Body != nil && req.Body != http.NoBody {
		bodyBytes, err := io.ReadAll(req.Body)
		_ = req.Body.Close() //nolint:errcheck
		if err == nil && len(bodyBytes) > 0 {
			req.Body = io.NopCloser(newBytesReader(bodyBytes))
			harReq.BodySize = len(bodyBytes)
			harReq.PostData = &HARPostData{
				MimeType: req.Header.Get("Content-Type"),
				Text:     string(bodyBytes),
			}
		}
	}

	return harReq
}

func buildHARResponse(resp *http.Response, body []byte) HARResponse {
	var headers []HARNameVal
	for k, vs := range resp.Header {
		for _, v := range vs {
			headers = append(headers, HARNameVal{Name: k, Value: v})
		}
	}

	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	return HARResponse{
		Status:      resp.StatusCode,
		StatusText:  resp.Status,
		HTTPVersion: "HTTP/1.1",
		Headers:     headers,
		Content: HARContent{
			Size:     len(body),
			MimeType: mimeType,
			Text:     string(body),
		},
		RedirectURL: resp.Header.Get("Location"),
		HeadersSize: -1,
		BodySize:    len(body),
	}
}
