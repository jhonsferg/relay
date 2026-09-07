package openapi

// White-box tests (package openapi, not openapi_test) that need direct
// access to unexported validatingTransport internals to observe what
// *http.Request actually reaches openapi3filter.ValidateRequest - not
// observable from the external test package, since AuthenticationFunc is
// always overwritten to NoopAuthenticationFunc unless WithStrict() is used,
// and even then there's no public hook to inject a custom one.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/jhonsferg/relay"
)

// securedSpec declares an API-key security requirement on GET /pets so
// openapi3filter.ValidateRequest actually invokes AuthenticationFunc,
// giving us a hook to observe RequestValidationInput.Request.
const securedSpec = `
openapi: "3.0.0"
info:
  title: Secured
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /pets:
    get:
      operationId: listPets
      security:
        - ApiKeyAuth: []
      responses:
        "200":
          description: ok
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: X-Api-Key
`

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// TestValidatingTransport_ValidatesAgainstRealRequest guards against
// passing findReq (host/scheme rewritten to the spec's declared server, for
// FindRoute matching only) to openapi3filter.ValidateRequest instead of the
// real request - a comment in RoundTrip states the intent ("validation
// uses the real URL") that the code previously contradicted.
func TestValidatingTransport_ValidatesAgainstRealRequest(t *testing.T) {
	doc, err := Load([]byte(securedSpec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("gorillamux.NewRouter: %v", err)
	}

	var capturedHost string
	filterOpts := &openapi3filter.Options{
		ExcludeRequestBody:    false,
		ExcludeResponseBody:   false,
		IncludeResponseStatus: true,
		AuthenticationFunc: func(_ context.Context, ai *openapi3filter.AuthenticationInput) error {
			capturedHost = ai.RequestValidationInput.Request.Host
			return nil // let validation proceed
		},
	}

	specServerURL, err := url.Parse(doc.Servers[0].URL)
	if err != nil {
		t.Fatalf("parse spec server URL: %v", err)
	}

	transport := &validatingTransport{
		base:          roundTripFunc(func(req *http.Request) (*http.Response, error) { return srv.Client().Do(req) }),
		router:        router,
		filterOpts:    filterOpts,
		cfg:           &option{},
		specServerURL: specServerURL,
	}

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithTransportMiddleware(func(_ http.RoundTripper) http.RoundTripper { return transport }),
		relay.WithDisableRetry(),
	)

	if _, err := client.Execute(client.Get("/pets")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if capturedHost == "" {
		t.Fatal("AuthenticationFunc was never called - test setup didn't exercise validation")
	}
	if capturedHost == "api.example.com" {
		t.Errorf("validation saw the spec's server host %q, want the real request host (findReq leaked into validation)", capturedHost)
	}
}

// erroringReadCloser fails every Read and records whether Close was called.
type erroringReadCloser struct{ closed bool }

func (e *erroringReadCloser) Read(_ []byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (e *erroringReadCloser) Close() error               { e.closed = true; return nil }

// TestValidatingTransport_ClosesBodyOnReadError guards against
// http.RoundTripper's contract violation: RoundTrip must always close
// req.Body, including on error paths. When io.ReadAll(req.Body) fails, the
// request is never forwarded to t.base, so nothing else would close it.
func TestValidatingTransport_ClosesBodyOnReadError(t *testing.T) {
	doc, err := Load([]byte(securedSpec))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("gorillamux.NewRouter: %v", err)
	}

	transport := &validatingTransport{
		base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Fatal("base transport should never be reached when the body read fails")
			return nil, nil
		}),
		router:     router,
		filterOpts: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc},
		cfg:        &option{},
	}

	body := &erroringReadCloser{}
	req, err := http.NewRequest(http.MethodGet, "https://api.example.com/pets", body)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}

	_, err = transport.RoundTrip(req)
	if err == nil {
		t.Fatal("expected an error from the failing body read, got nil")
	}
	if !body.closed {
		t.Error("req.Body was not closed when io.ReadAll failed")
	}
}
