package relay

import (
	"fmt"
	"net/http"
	"testing"
)

// recordingTransport records the last request scheme it saw.
type recordingTransport struct {
	lastScheme string
	resp       *http.Response
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.lastScheme = req.URL.Scheme
	if r.resp != nil {
		return r.resp, nil
	}
	return nil, fmt.Errorf("recordingTransport: no response configured")
}

func TestSchemeRouter_CustomScheme(t *testing.T) {
	custom := &recordingTransport{}
	fallback := &recordingTransport{}

	router := &schemeRouter{
		adapters: map[string]http.RoundTripper{"myproto": custom},
		fallback: fallback,
	}

	req, _ := http.NewRequest(http.MethodGet, "myproto://host/path", nil)
	_, _ = router.RoundTrip(req) //nolint:errcheck
	if custom.lastScheme != "myproto" {
		t.Errorf("expected custom transport for myproto, got scheme %q", custom.lastScheme)
	}
	if fallback.lastScheme != "" {
		t.Error("fallback should not have been called for myproto")
	}
}

func TestSchemeRouter_FallsBackForHTTPS(t *testing.T) {
	custom := &recordingTransport{}
	fallback := &recordingTransport{}

	router := &schemeRouter{
		adapters: map[string]http.RoundTripper{"myproto": custom},
		fallback: fallback,
	}

	req, _ := http.NewRequest(http.MethodGet, "https://host/path", nil)
	_, _ = router.RoundTrip(req) //nolint:errcheck
	if fallback.lastScheme != "https" {
		t.Errorf("expected fallback for https, got scheme %q", fallback.lastScheme)
	}
	if custom.lastScheme != "" {
		t.Error("custom should not have been called for https")
	}
}

func TestSchemeRouter_FallsBackForHTTP(t *testing.T) {
	custom := &recordingTransport{}
	fallback := &recordingTransport{}

	router := &schemeRouter{
		adapters: map[string]http.RoundTripper{"custom": custom},
		fallback: fallback,
	}

	req, _ := http.NewRequest(http.MethodGet, "http://host/path", nil)
	_, _ = router.RoundTrip(req) //nolint:errcheck
	if fallback.lastScheme != "http" {
		t.Errorf("expected fallback for http, got %q", fallback.lastScheme)
	}
}

func TestSchemeRouter_NilURL(t *testing.T) {
	router := &schemeRouter{
		adapters: map[string]http.RoundTripper{},
		fallback: &recordingTransport{},
	}
	req := &http.Request{} // URL is nil
	_, err := router.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error for nil URL")
	}
}

func TestWithTransportAdapter_RegistersAdapter(t *testing.T) {
	rt := &recordingTransport{}
	c := New(WithTransportAdapter("myproto", rt))
	if c.config.SchemeAdapters == nil {
		t.Fatal("expected SchemeAdapters to be initialised")
	}
	if c.config.SchemeAdapters["myproto"] != rt {
		t.Error("expected myproto adapter to be registered")
	}
}

func TestWithTransportAdapter_MultipleSchemes(t *testing.T) {
	rt1 := &recordingTransport{}
	rt2 := &recordingTransport{}
	c := New(
		WithTransportAdapter("proto1", rt1),
		WithTransportAdapter("proto2", rt2),
	)
	if c.config.SchemeAdapters["proto1"] != rt1 {
		t.Error("proto1 adapter missing")
	}
	if c.config.SchemeAdapters["proto2"] != rt2 {
		t.Error("proto2 adapter missing")
	}
}

func TestWithTransportAdapter_CloneIsolation(t *testing.T) {
	rt1 := &recordingTransport{}
	parent := New(WithTransportAdapter("myproto", rt1))
	child := parent.With()

	// Mutate child; parent should be unaffected.
	rt2 := &recordingTransport{}
	child.config.SchemeAdapters["other"] = rt2

	if _, ok := parent.config.SchemeAdapters["other"]; ok {
		t.Error("parent SchemeAdapters should be independent of child")
	}
}
