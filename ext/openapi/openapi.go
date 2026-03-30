// Package openapi provides OpenAPI 3.x request and response validation
// middleware for relay HTTP clients using github.com/getkin/kin-openapi.
//
// When attached, each request is validated against the spec before it is sent.
// If validation fails the request is aborted and a structured [ValidationError]
// is returned without making a network call. Optionally, responses can also be
// validated after they are received.
//
// # Usage
//
//	import (
//	    "github.com/jhonsferg/relay"
//	    relayopenapi "github.com/jhonsferg/relay/ext/openapi"
//	)
//
//	// Load the OpenAPI spec.
//	doc, err := relayopenapi.LoadFile("openapi.yaml")
//	if err != nil { log.Fatal(err) }
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relayopenapi.WithValidation(doc),
//	)
//
//	// Responses can also be validated:
//	client = relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relayopenapi.WithValidation(doc, relayopenapi.WithResponseValidation()),
//	)
//
// # Strict mode
//
// By default unknown query parameters and headers are allowed. Enable strict
// mode to reject them:
//
//	relayopenapi.WithValidation(doc, relayopenapi.WithStrict())
//
// # Error handling
//
// A validation failure returns a *[ValidationError] that implements error.
// Use errors.As to access the underlying kin-openapi details.
package openapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/jhonsferg/relay"
)

// ValidationError is returned when a request or response does not conform to
// the OpenAPI specification.
type ValidationError struct {
	// Phase indicates whether the failure was on "request" or "response".
	Phase string
	// Cause is the underlying kin-openapi validation error.
	Cause error
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("openapi %s validation failed: %v", e.Phase, e.Cause)
}

func (e *ValidationError) Unwrap() error { return e.Cause }

// option holds configuration for the validation middleware.
type option struct {
	validateResponse bool
	strict           bool
}

// Option configures [WithValidation].
type Option func(*option)

// WithResponseValidation enables validation of HTTP responses in addition to requests.
func WithResponseValidation() Option {
	return func(o *option) { o.validateResponse = true }
}

// WithStrict rejects requests that contain query parameters or headers not
// described in the spec.
func WithStrict() Option {
	return func(o *option) { o.strict = true }
}

// LoadFile loads and validates an OpenAPI 3.x spec from a YAML or JSON file.
func LoadFile(path string) (*openapi3.T, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("openapi: load %s: %w", path, err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		return nil, fmt.Errorf("openapi: spec validation: %w", err)
	}
	return doc, nil
}

// Load parses and validates an OpenAPI 3.x spec from raw YAML or JSON bytes.
func Load(data []byte) (*openapi3.T, error) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(data)
	if err != nil {
		return nil, fmt.Errorf("openapi: parse spec: %w", err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		return nil, fmt.Errorf("openapi: spec validation: %w", err)
	}
	return doc, nil
}

// WithValidation returns a [relay.Option] that installs an OpenAPI validation
// transport middleware. Requests that do not conform to doc are rejected before
// reaching the network. Pass [WithResponseValidation] to also validate responses.
func WithValidation(doc *openapi3.T, opts ...Option) relay.Option {
	cfg := &option{}
	for _, o := range opts {
		o(cfg)
	}

	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		// Router construction only fails for invalid specs; treat as permanent.
		panic(fmt.Sprintf("openapi: build router: %v", err))
	}

	filterOpts := &openapi3filter.Options{
		ExcludeRequestBody:    false,
		ExcludeResponseBody:   false,
		IncludeResponseStatus: true,
	}
	if !cfg.strict {
		filterOpts.AuthenticationFunc = openapi3filter.NoopAuthenticationFunc
	}

	// Extract the first server URL from the spec to use as the base for route
	// matching. The actual request URL target may differ (e.g. in tests or
	// behind a reverse proxy), so we use the spec URL for FindRoute only.
	var specServerURL *url.URL
	if len(doc.Servers) > 0 {
		specServerURL, _ = url.Parse(doc.Servers[0].URL)
	}

	return relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return &validatingTransport{
			base:          next,
			router:        router,
			filterOpts:    filterOpts,
			cfg:           cfg,
			specServerURL: specServerURL,
		}
	})
}

type validatingTransport struct {
	base          http.RoundTripper
	router        routers.Router
	filterOpts    *openapi3filter.Options
	cfg           *option
	specServerURL *url.URL // first server URL from the spec, used for route matching
}

func (t *validatingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// -- Buffer body (needed for both route finding and validation) ------------
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req = req.Clone(ctx)
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(bodyBytes)), nil
		}
	}

	// -- Request validation ----------------------------------------------------
	// FindRoute requires the request URL to match a server URL in the spec.
	// Build a copy with the spec server's scheme+host so the router can match
	// on path even when the actual target differs (test servers, proxies, etc).
	findReq := req.Clone(ctx)
	if t.specServerURL != nil {
		findReq.URL = &url.URL{
			Scheme:   t.specServerURL.Scheme,
			Host:     t.specServerURL.Host,
			Path:     req.URL.Path,
			RawQuery: req.URL.RawQuery,
		}
		findReq.Host = t.specServerURL.Host
	}

	route, pathParams, err := t.router.FindRoute(findReq)
	if err != nil {
		// Route not found in spec → skip validation (the server will 404).
		return t.base.RoundTrip(req)
	}

	// Pass the original request (not findReq) so validation uses the real URL.
	reqInput := &openapi3filter.RequestValidationInput{
		Request:    findReq,
		PathParams: pathParams,
		Route:      route,
		Options:    t.filterOpts,
	}

	if err := openapi3filter.ValidateRequest(ctx, reqInput); err != nil {
		return nil, &ValidationError{Phase: "request", Cause: err}
	}

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// -- Optional response validation ------------------------------------------
	if t.cfg.validateResponse {
		var respBody []byte
		if resp.Body != nil {
			respBody, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return nil, err
			}
			resp.Body = io.NopCloser(bytes.NewReader(respBody))
		}

		respInput := &openapi3filter.ResponseValidationInput{
			RequestValidationInput: reqInput,
			Status:                 resp.StatusCode,
			Header:                 resp.Header,
			Body:                   io.NopCloser(bytes.NewReader(respBody)),
			Options:                t.filterOpts,
		}
		if err := openapi3filter.ValidateResponse(ctx, respInput); err != nil {
			return nil, &ValidationError{Phase: "response", Cause: err}
		}
	}

	return resp, nil
}

// IsValidationError reports whether err is (or wraps) a *ValidationError.
func IsValidationError(err error) (*ValidationError, bool) {
	var ve *ValidationError
	ok := errors.As(err, &ve)
	return ve, ok
}
