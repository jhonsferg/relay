package relay

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"golang.org/x/sync/singleflight"
)

// coalesceTransport deduplicates concurrent identical idempotent requests so
// that only one real HTTP call is made. All waiting callers receive independent
// copies of the response body.
type coalesceTransport struct {
	base  http.RoundTripper
	group singleflight.Group
}

func newCoalesceTransport(base http.RoundTripper) http.RoundTripper {
	return &coalesceTransport{base: base}
}

// RoundTrip deduplicates GET and HEAD requests with the same key. Each caller
// receives its own copy of the response body so they can read independently.
func (t *coalesceTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Only deduplicate idempotent methods.
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return t.base.RoundTrip(req)
	}

	key := coalesceKey(req)

	type result struct {
		resp *http.Response
		body []byte
	}

	v, err, _ := t.group.Do(key, func() (any, error) {
		resp, err := t.base.RoundTrip(req)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close() //nolint:errcheck
		if readErr != nil {
			return nil, readErr
		}
		return &result{resp: resp, body: body}, nil
	})

	if err != nil {
		return nil, err
	}

	r := v.(*result)

	// Clone the response and give each caller its own body reader.
	cloned := *r.resp
	cloned.Body = io.NopCloser(bytes.NewReader(r.body))
	return &cloned, nil
}

// coalesceKey builds a stable string key from the request method, URL, and a
// sorted subset of headers relevant to caching identity.
func coalesceKey(req *http.Request) string {
	var sb strings.Builder
	sb.WriteString(req.Method)
	sb.WriteByte('|')
	sb.WriteString(req.URL.String())

	// Include relevant headers (Authorization, Accept) sorted for stability.
	var relevantHeaders []string
	for _, h := range []string{"Authorization", "Accept", "Accept-Language"} {
		if v := req.Header.Get(h); v != "" {
			relevantHeaders = append(relevantHeaders, fmt.Sprintf("%s=%s", h, v))
		}
	}
	sort.Strings(relevantHeaders)
	for _, h := range relevantHeaders {
		sb.WriteByte('|')
		sb.WriteString(h)
	}
	return sb.String()
}
