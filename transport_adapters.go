package relay

import (
	"fmt"
	"net/http"
)

// schemeRouter is an [http.RoundTripper] that dispatches requests to a
// registered adapter based on the URL scheme. For schemes not found in the
// adapter map it falls back to the provided default transport (used for
// http/https). This allows custom protocols to be handled by dedicated
// transports without modifying the default HTTP stack.
type schemeRouter struct {
	// adapters maps URL scheme -> transport. Never mutated after construction.
	adapters map[string]http.RoundTripper

	// fallback handles all schemes not present in adapters (typically
	// the standard *http.Transport or the full middleware-wrapped stack).
	fallback http.RoundTripper
}

// RoundTrip dispatches req to the adapter registered for req.URL.Scheme.
// If no adapter is registered, the fallback transport is used.
func (r *schemeRouter) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil {
		return nil, fmt.Errorf("transport_adapter: request URL is nil")
	}
	if rt, ok := r.adapters[req.URL.Scheme]; ok {
		return rt.RoundTrip(req)
	}
	return r.fallback.RoundTrip(req)
}
