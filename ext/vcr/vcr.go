// Package vcr provides HTTP interaction recording and playback for testing.
// Inspired by the VCR gem from Ruby.
package vcr

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jhonsferg/relay"
)

// Mode controls VCR recording/playback.
type Mode string

const (
	// ModeRecord records real requests to cassette.
	ModeRecord Mode = "record"
	// ModePlayback replays from cassette, errors if not found.
	ModePlayback Mode = "playback"
	// ModePassthrough disables VCR, passes through real requests.
	ModePassthrough Mode = "passthrough"
)

// Cassette holds recorded HTTP interactions.
type Cassette struct {
	Interactions []Interaction `json:"interactions"`
}

// Interaction is a recorded request-response pair.
type Interaction struct {
	Request  RecordedRequest  `json:"request"`
	Response RecordedResponse `json:"response"`
}

// RecordedRequest is a request that was recorded.
type RecordedRequest struct {
	Method string              `json:"method"`
	URL    string              `json:"url"`
	Header map[string][]string `json:"header,omitempty"`
	Body   string              `json:"body,omitempty"`
}

// MarshalJSON base64-encodes Body so binary bodies (images, protobuf, gzip)
// round-trip byte-for-byte instead of being corrupted by encoding/json's
// replacement of invalid UTF-8 sequences with U+FFFD when marshaling a Go
// string directly.
func (r RecordedRequest) MarshalJSON() ([]byte, error) {
	return json.Marshal(recordedRequestJSON{
		Method:       r.Method,
		URL:          r.URL,
		Header:       r.Header,
		Body:         base64.StdEncoding.EncodeToString([]byte(r.Body)),
		BodyEncoding: bodyEncodingBase64,
	})
}

// UnmarshalJSON decodes Body according to its BodyEncoding. An empty
// BodyEncoding means the cassette was written before this change (Body is
// literal text), which is preserved for backward compatibility with
// existing cassette files.
func (r *RecordedRequest) UnmarshalJSON(data []byte) error {
	var raw recordedRequestJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	body, err := decodeBody(raw.Body, raw.BodyEncoding)
	if err != nil {
		return fmt.Errorf("vcr: request body: %w", err)
	}
	r.Method = raw.Method
	r.URL = raw.URL
	r.Header = raw.Header
	r.Body = body
	return nil
}

// recordedRequestJSON is the on-disk shape of RecordedRequest.
type recordedRequestJSON struct {
	Method       string       `json:"method"`
	URL          string       `json:"url"`
	Header       headerValues `json:"header,omitempty"`
	Body         string       `json:"body,omitempty"`
	BodyEncoding string       `json:"body_encoding,omitempty"`
}

// RecordedResponse is a response that was recorded.
type RecordedResponse struct {
	Status int                 `json:"status"`
	Header map[string][]string `json:"header,omitempty"`
	Body   string              `json:"body"`
}

// MarshalJSON base64-encodes Body; see RecordedRequest.MarshalJSON.
func (r RecordedResponse) MarshalJSON() ([]byte, error) {
	return json.Marshal(recordedResponseJSON{
		Status:       r.Status,
		Header:       r.Header,
		Body:         base64.StdEncoding.EncodeToString([]byte(r.Body)),
		BodyEncoding: bodyEncodingBase64,
	})
}

// UnmarshalJSON decodes Body according to its BodyEncoding; see
// RecordedRequest.UnmarshalJSON.
func (r *RecordedResponse) UnmarshalJSON(data []byte) error {
	var raw recordedResponseJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	body, err := decodeBody(raw.Body, raw.BodyEncoding)
	if err != nil {
		return fmt.Errorf("vcr: response body: %w", err)
	}
	r.Status = raw.Status
	r.Header = raw.Header
	r.Body = body
	return nil
}

// recordedResponseJSON is the on-disk shape of RecordedResponse.
type recordedResponseJSON struct {
	Status       int          `json:"status"`
	Header       headerValues `json:"header,omitempty"`
	Body         string       `json:"body"`
	BodyEncoding string       `json:"body_encoding,omitempty"`
}

// headerValues is the on-disk representation of recorded HTTP headers.
// Marshals as the standard {"X-Foo": ["a","b"]} multi-value shape (matching
// http.Header semantics - a repeated header, e.g. multiple Set-Cookie
// values, is common and was previously silently collapsed to just the
// first value). UnmarshalJSON also accepts the older {"X-Foo": "a"}
// single-value shape written by cassettes recorded before multi-value
// support, for backward compatibility.
type headerValues map[string][]string

func (h *headerValues) UnmarshalJSON(data []byte) error {
	var multi map[string][]string
	if err := json.Unmarshal(data, &multi); err == nil {
		*h = multi
		return nil
	}
	var single map[string]string
	if err := json.Unmarshal(data, &single); err != nil {
		return err
	}
	multi = make(map[string][]string, len(single))
	for k, v := range single {
		multi[k] = []string{v}
	}
	*h = multi
	return nil
}

// bodyEncodingBase64 marks a cassette body field as base64-encoded. Cassette
// files written before this change have no body_encoding field at all,
// which decodeBody treats as literal plain text for backward compatibility.
const bodyEncodingBase64 = "base64"

// decodeBody decodes a cassette body field according to its encoding marker.
func decodeBody(body, encoding string) (string, error) {
	switch encoding {
	case "", "plain":
		return body, nil
	case bodyEncodingBase64:
		decoded, err := base64.StdEncoding.DecodeString(body)
		if err != nil {
			return "", fmt.Errorf("invalid base64 body: %w", err)
		}
		return string(decoded), nil
	default:
		return "", fmt.Errorf("unknown body encoding %q", encoding)
	}
}

// VCR is the cassette player/recorder.
type VCR struct {
	mode         Mode
	cassettePath string
	cassette     *Cassette
	mu           sync.Mutex
	playbackIdx  int
	saveCount    int // number of times saveUnlocked has actually written to disk; used by tests
}

// saveDebounceThreshold is the interaction count above which cassette saves
// are batched instead of happening after every single interaction. Below
// this threshold - the common case, a handful of interactions in a typical
// test - every interaction is still saved immediately, preserving full
// crash resilience ("if a test fails, previous requests are still
// recorded"). Above it, saving after every single interaction makes
// recording a large session (hundreds of interactions) needlessly slow:
// each save re-marshals and rewrites the *entire* cassette from scratch, so
// per-call cost is O(n) and total cost across n interactions is O(n²).
const saveDebounceThreshold = 20

// saveDebounceEvery is how many interactions accumulate between saves once
// saveDebounceThreshold is exceeded. Any interactions recorded after the
// last debounced save are not guaranteed to be on disk until the caller
// explicitly calls [VCR.Save] - already the documented pattern for ending a
// recording session.
const saveDebounceEvery = 10

// shouldSaveAfterAppend reports whether the cassette should be persisted to
// disk immediately after it has grown to hold n interactions.
func shouldSaveAfterAppend(n int) bool {
	if n <= saveDebounceThreshold {
		return true
	}
	return n%saveDebounceEvery == 0
}

// New creates a VCR for the given cassette file.
func New(cassettePath string, mode Mode) (*VCR, error) {
	vcr := &VCR{
		mode:         mode,
		cassettePath: cassettePath,
		cassette:     &Cassette{Interactions: []Interaction{}},
		playbackIdx:  0,
	}

	switch mode {
	case ModePlayback:
		if err := vcr.load(); err != nil {
			return nil, fmt.Errorf("failed to load cassette: %w", err)
		}
	case ModeRecord:
		// Try to load an existing cassette to append to. load() already
		// treats a missing file as non-fatal (returns nil); any other error
		// (e.g. a corrupted/truncated cassette) must not be silently
		// swallowed - otherwise ModeRecord would silently start from an
		// empty cassette and overwrite the corrupted file on the next save.
		if err := vcr.load(); err != nil {
			return nil, fmt.Errorf("failed to load existing cassette: %w", err)
		}
	}

	return vcr, nil
}

// load reads the cassette from disk.
func (v *VCR) load() error {
	data, err := os.ReadFile(v.cassettePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	var c Cassette
	if err := json.Unmarshal(data, &c); err != nil {
		return err
	}

	v.cassette = &c
	return nil
}

// Save writes the cassette to disk.
func (v *VCR) Save() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.saveUnlocked()
}

// saveUnlocked writes cassette to disk without acquiring lock (caller must hold lock).
func (v *VCR) saveUnlocked() error {
	if v.mode == ModePassthrough {
		return nil
	}

	dir := filepath.Dir(v.cassettePath)
	if err := os.MkdirAll(dir, 0o700); err != nil { //nolint:gosec
		return err
	}

	data, err := json.MarshalIndent(v.cassette, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(v.cassettePath, data, 0o600); err != nil { //nolint:gosec
		return err
	}
	v.saveCount++
	return nil
}

// Middleware returns a relay transport middleware for recording/playback.
func (v *VCR) Middleware() relay.Option {
	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &vcrTransport{vcr: v, base: next}
	})
}

type vcrTransport struct {
	vcr  *VCR
	base http.RoundTripper
}

func (t *vcrTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	switch t.vcr.mode {
	case ModeRecord:
		return t.recordRoundTrip(req)
	case ModePlayback:
		return t.playbackRoundTrip(req)
	case ModePassthrough:
		return t.base.RoundTrip(req)
	default:
		return t.base.RoundTrip(req)
	}
}

func (t *vcrTransport) recordRoundTrip(req *http.Request) (*http.Response, error) {
	// Read request body for recording
	var reqBody string
	if req.Body != nil {
		bodyBytes, err := io.ReadAll(req.Body)
		// The original body is being replaced below regardless of outcome
		// (success or error) - close it now since http.RoundTripper's
		// contract requires the body to always be closed, and this was the
		// last reference to it.
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
		reqBody = string(bodyBytes)
		// Restore body for actual request
		req.Body = io.NopCloser(strings.NewReader(reqBody))
	}

	// Execute real request
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	// Read response body
	respBodyBytes, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	respBody := string(respBodyBytes)
	// Restore body
	resp.Body = io.NopCloser(strings.NewReader(respBody))

	// Record interaction
	t.vcr.mu.Lock()
	defer t.vcr.mu.Unlock()

	interaction := Interaction{
		Request: RecordedRequest{
			Method: req.Method,
			URL:    req.URL.String(),
			Header: headerMapFromHeader(req.Header),
			Body:   reqBody,
		},
		Response: RecordedResponse{
			Status: resp.StatusCode,
			Header: headerMapFromHeader(resp.Header),
			Body:   respBody,
		},
	}

	t.vcr.cassette.Interactions = append(t.vcr.cassette.Interactions, interaction)

	// Save cassette (using unlocked version since we hold the lock), unless
	// this recording session is large enough that shouldSaveAfterAppend
	// decides to debounce - see saveDebounceThreshold. Return resp even on
	// save failure: the network round-trip succeeded and the caller should
	// not be penalised with a nil response for a disk write error.
	if shouldSaveAfterAppend(len(t.vcr.cassette.Interactions)) {
		if err := t.vcr.saveUnlocked(); err != nil {
			return resp, fmt.Errorf("vcr: cassette save failed (response still returned): %w", err)
		}
	}

	return resp, nil
}

func (t *vcrTransport) playbackRoundTrip(req *http.Request) (*http.Response, error) {
	t.vcr.mu.Lock()
	defer t.vcr.mu.Unlock()

	// Find matching interaction
	for i := t.vcr.playbackIdx; i < len(t.vcr.cassette.Interactions); i++ {
		interaction := t.vcr.cassette.Interactions[i]
		if interaction.Request.Method == req.Method && interaction.Request.URL == req.URL.String() {
			t.vcr.playbackIdx = i + 1

			// Create response from recorded data
			return &http.Response{
				Status:     fmt.Sprintf("%d %s", interaction.Response.Status, http.StatusText(interaction.Response.Status)),
				StatusCode: interaction.Response.Status,
				Proto:      "HTTP/1.1",
				ProtoMajor: 1,
				ProtoMinor: 1,
				Header:     headerFromMap(interaction.Response.Header),
				Body:       io.NopCloser(strings.NewReader(interaction.Response.Body)),
				Request:    req,
			}, nil
		}
	}

	return nil, fmt.Errorf("vcr: no matching interaction found for %s %s", req.Method, req.URL.String())
}

func headerMapFromHeader(h http.Header) map[string][]string {
	m := make(map[string][]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			m[k] = append([]string(nil), v...)
		}
	}
	return m
}

func headerFromMap(m map[string][]string) http.Header {
	h := make(http.Header, len(m))
	for k, v := range m {
		for _, val := range v {
			h.Add(k, val)
		}
	}
	return h
}
