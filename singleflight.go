package relay

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"golang.org/x/sync/singleflight"
)

// DeduplicationConfig controls request deduplication behaviour.
type DeduplicationConfig struct {
	// Enabled activates singleflight deduplication for safe methods (GET, HEAD).
	// Default: false.
	Enabled bool
}

// deduplicationOverrideKey is the context key for per-request deduplication overrides.
type deduplicationOverrideKey struct{}

// deduplicator wraps an http.RoundTripper with singleflight semantics.
type deduplicator struct {
	group     singleflight.Group
	transport http.RoundTripper
}

func newDeduplicator(transport http.RoundTripper) *deduplicator {
	return &deduplicator{transport: transport}
}

func (d *deduplicator) RoundTrip(req *http.Request) (*http.Response, error) {
	// Check for per-request override via context.
	if override, ok := req.Context().Value(deduplicationOverrideKey{}).(bool); ok && !override {
		return d.transport.RoundTrip(req)
	}

	// Only deduplicate safe methods.
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return d.transport.RoundTrip(req)
	}

	key := req.Method + " " + req.URL.String()

	type result struct {
		body   []byte
		header http.Header
		status int
		proto  string
	}

	v, err, _ := d.group.Do(key, func() (interface{}, error) {
		// Use a detached context so one caller's cancellation does not abort
		// the shared request.
		detached := req.WithContext(newDetachedContext(req.Context()))
		resp, err := d.transport.RoundTrip(detached)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close() //nolint:errcheck
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		return &result{
			body:   body,
			header: resp.Header.Clone(),
			status: resp.StatusCode,
			proto:  resp.Proto,
		}, nil
	})
	if err != nil {
		return nil, err
	}

	r := v.(*result)
	return &http.Response{
		StatusCode:    r.status,
		Proto:         r.proto,
		Header:        r.header.Clone(),
		Body:          io.NopCloser(bytes.NewReader(r.body)),
		ContentLength: int64(len(r.body)),
		Request:       req,
	}, nil
}

// detachedContext wraps a parent context, propagating values but never
// propagating cancellation or deadline. This ensures that when one caller
// cancels its context, the shared singleflight request continues for all
// other waiters.
type detachedContext struct {
	parent context.Context
}

func newDetachedContext(parent context.Context) context.Context {
	return detachedContext{parent: parent}
}

func (d detachedContext) Deadline() (time.Time, bool)       { return time.Time{}, false }
func (d detachedContext) Done() <-chan struct{}              { return nil }
func (d detachedContext) Err() error                        { return nil }
func (d detachedContext) Value(key interface{}) interface{} { return d.parent.Value(key) }
