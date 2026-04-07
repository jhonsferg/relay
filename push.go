package relay

import (
	"net/http"
	"sync"
)

// PushPromiseHandler is called when the server sends an HTTP/2 push promise.
// pushedURL is the URL of the pushed resource; pushedResp is the pushed
// response whose body the handler is responsible for consuming and closing.
//
// Note: active push-promise interception requires golang.org/x/net/http2
// v0.x or earlier (the PushHandler interface was removed in later versions).
// On the current dependency (golang.org/x/net v0.50.0) the handler is
// registered but never invoked because the underlying transport disables
// server push at the HTTP/2 SETTINGS level. The type is kept in the public
// API so callers can prepare for future transport support without breaking
// changes.
type PushPromiseHandler func(pushedURL string, pushedResp *http.Response)

// PushedResponseCache stores push-promised responses indexed by URL.
// It is safe for concurrent use.
type PushedResponseCache struct {
	mu      sync.RWMutex
	entries map[string]*http.Response
}

// NewPushedResponseCache returns an initialised, empty [PushedResponseCache].
func NewPushedResponseCache() *PushedResponseCache {
	return &PushedResponseCache{entries: make(map[string]*http.Response)}
}

// Store stores a pushed response keyed by its URL. Any previously stored
// response for the same URL is silently replaced.
func (c *PushedResponseCache) Store(url string, resp *http.Response) {
	c.mu.Lock()
	c.entries[url] = resp
	c.mu.Unlock()
}

// Load retrieves and removes the pushed response for url.
// Returns nil, false when no entry exists.
func (c *PushedResponseCache) Load(url string) (*http.Response, bool) {
	c.mu.Lock()
	resp, ok := c.entries[url]
	if ok {
		delete(c.entries, url)
	}
	c.mu.Unlock()
	return resp, ok
}

// Len returns the number of entries currently held in the cache.
func (c *PushedResponseCache) Len() int {
	c.mu.RLock()
	n := len(c.entries)
	c.mu.RUnlock()
	return n
}

// WithHTTP2PushHandler registers handler to be called whenever the server
// sends an HTTP/2 push promise.
//
// # Current limitation
//
// golang.org/x/net v0.50.0 (the version used by this module) removed the
// public PushHandler interface from http2.Transport and the client-side
// SETTINGS frame explicitly disables server push (SETTINGS_ENABLE_PUSH=0).
// As a result this option is a no-op at runtime: the handler is stored in
// the Config for forward-compatibility but is never invoked. Once the
// upstream transport re-exposes a push interception API the stored handler
// will be wired in without any change to call-sites.
//
// If you pass a [*PushedResponseCache] as your handler via a closure, relay
// will automatically serve subsequent requests from the cache when the URL
// matches a pushed response.
func WithHTTP2PushHandler(handler PushPromiseHandler) Option {
	return func(c *Config) {
		c.HTTP2PushHandler = handler
	}
}
