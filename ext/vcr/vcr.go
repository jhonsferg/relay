// Package vcr provides HTTP interaction recording and playback for testing.
// Inspired by the VCR gem from Ruby.
package vcr

import (
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
	Method string            `json:"method"`
	URL    string            `json:"url"`
	Header map[string]string `json:"header,omitempty"`
	Body   string            `json:"body,omitempty"`
}

// RecordedResponse is a response that was recorded.
type RecordedResponse struct {
	Status int               `json:"status"`
	Header map[string]string `json:"header,omitempty"`
	Body   string            `json:"body"`
}

// VCR is the cassette player/recorder.
type VCR struct {
	mode         Mode
	cassettePath string
	cassette     *Cassette
	mu           sync.Mutex
	playbackIdx  int
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
		// Try to load existing cassette if it exists
		_ = vcr.load()
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

	return os.WriteFile(v.cassettePath, data, 0o600) //nolint:gosec
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

	// Save cassette immediately (using unlocked version since we hold the lock)
	if err := t.vcr.saveUnlocked(); err != nil {
		return nil, err
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

func headerMapFromHeader(h http.Header) map[string]string {
	m := make(map[string]string)
	for k, v := range h {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	return m
}

func headerFromMap(m map[string]string) http.Header {
	h := make(http.Header)
	for k, v := range m {
		h.Set(k, v)
	}
	return h
}
