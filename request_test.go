package relay

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

func TestRequest_WithContext(t *testing.T) {
	t.Parallel()
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "val")
	req := newRequest(http.MethodGet, "/").WithContext(ctx)
	if req.ctx.Value(ctxKey{}) != "val" {
		t.Error("WithContext did not set context")
	}
}

func TestRequest_WithTimeout(t *testing.T) {
	t.Parallel()
	req := newRequest(http.MethodGet, "/").WithTimeout(5 * time.Second)
	if req.timeout != 5*time.Second {
		t.Errorf("expected 5s timeout, got %v", req.timeout)
	}
}

func TestRequest_WithPathParam(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Get(srv.URL()+"/users/{id}").WithPathParam("id", "42")
	c.Execute(req) //nolint:errcheck
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Path != "/users/42" {
		t.Errorf("expected path /users/42, got %q", rec.Path)
	}
}

func TestRequest_WithPathParams(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Get(srv.URL() + "/orgs/{org}/users/{id}").WithPathParams(map[string]string{
		"org": "acme",
		"id":  "99",
	})
	c.Execute(req) //nolint:errcheck
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Path != "/orgs/acme/users/99" {
		t.Errorf("expected /orgs/acme/users/99, got %q", rec.Path)
	}
}

func TestRequest_WithTag(t *testing.T) {
	t.Parallel()
	req := newRequest(http.MethodGet, "/").WithTag("op", "list").WithTag("team", "platform")
	if req.Tag("op") != "list" {
		t.Errorf("expected tag op=list, got %q", req.Tag("op"))
	}
	if req.Tag("team") != "platform" {
		t.Errorf("expected tag team=platform, got %q", req.Tag("team"))
	}
	if req.Tag("missing") != "" {
		t.Error("expected empty string for missing tag")
	}
}

func TestRequest_Tags(t *testing.T) {
	t.Parallel()
	req := newRequest(http.MethodGet, "/")
	if req.Tags() != nil {
		t.Error("expected nil tags when none set")
	}
	req.WithTag("k", "v")
	tags := req.Tags()
	if tags["k"] != "v" {
		t.Errorf("expected tag k=v, got %q", tags["k"])
	}
}

func TestRequest_WithHeaders(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Get(srv.URL() + "/").WithHeaders(map[string]string{
		"X-A": "1",
		"X-B": "2",
	})
	c.Execute(req) //nolint:errcheck
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Headers.Get("X-A") != "1" || rec.Headers.Get("X-B") != "2" {
		t.Error("expected X-A and X-B headers to be set")
	}
}

func TestRequest_WithQueryParams(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Get(srv.URL() + "/").WithQueryParams(map[string]string{
		"a": "1",
		"b": "2",
	})
	c.Execute(req) //nolint:errcheck
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Query.Get("a") != "1" || rec.Query.Get("b") != "2" {
		t.Errorf("expected a=1&b=2, got %v", rec.Query)
	}
}

func TestRequest_WithQueryParamValues(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Get(srv.URL()+"/").WithQueryParamValues("ids", []string{"1", "2", "3"})
	c.Execute(req) //nolint:errcheck
	rec, _ := srv.TakeRequest(time.Second)
	if len(rec.Query["ids"]) != 3 {
		t.Errorf("expected 3 ids values, got %v", rec.Query["ids"])
	}
}

func TestRequest_WithContentType(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Post(srv.URL() + "/").WithBody([]byte("data")).WithContentType("text/plain")
	c.Execute(req) //nolint:errcheck
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Headers.Get("Content-Type") != "text/plain" {
		t.Errorf("expected Content-Type: text/plain, got %q", rec.Headers.Get("Content-Type"))
	}
}

func TestRequest_WithAccept(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Get(srv.URL() + "/").WithAccept("application/xml")
	c.Execute(req) //nolint:errcheck
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Headers.Get("Accept") != "application/xml" {
		t.Errorf("expected Accept: application/xml, got %q", rec.Headers.Get("Accept"))
	}
}

func TestRequest_WithUserAgent(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Get(srv.URL() + "/").WithUserAgent("relay-test/1.0")
	c.Execute(req) //nolint:errcheck
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Headers.Get("User-Agent") != "relay-test/1.0" {
		t.Errorf("expected User-Agent: relay-test/1.0, got %q", rec.Headers.Get("User-Agent"))
	}
}

func TestRequest_WithRequestID(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Get(srv.URL() + "/").WithRequestID("req-abc-123")
	c.Execute(req) //nolint:errcheck
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Headers.Get("X-Request-Id") != "req-abc-123" {
		t.Errorf("expected X-Request-Id: req-abc-123, got %q", rec.Headers.Get("X-Request-Id"))
	}
}

func TestRequest_WithAPIKey(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Get(srv.URL()+"/").WithAPIKey("X-API-Key", "secret123")
	c.Execute(req) //nolint:errcheck
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Headers.Get("X-API-Key") != "secret123" {
		t.Errorf("expected X-API-Key: secret123, got %q", rec.Headers.Get("X-API-Key"))
	}
}

func TestRequest_WithBodyReader(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Post(srv.URL() + "/").WithBodyReader(bytes.NewBufferString("hello from reader"))
	c.Execute(req) //nolint:errcheck
	rec, _ := srv.TakeRequest(time.Second)
	if string(rec.Body) != "hello from reader" {
		t.Errorf("expected body 'hello from reader', got %q", string(rec.Body))
	}
}

func TestRequest_WithFormData(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Post(srv.URL() + "/form").WithFormData(map[string]string{
		"username": "alice",
		"role":     "admin",
	})
	c.Execute(req) //nolint:errcheck
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Headers.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Errorf("expected form content type, got %q", rec.Headers.Get("Content-Type"))
	}
	body := string(rec.Body)
	if !strings.Contains(body, "username=alice") {
		t.Errorf("expected username=alice in form body, got %q", body)
	}
}

func TestRequest_WithMultipart(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Post(srv.URL() + "/upload").WithMultipart([]MultipartField{
		{FieldName: "note", Content: []byte("hello")},
		{FieldName: "file", FileName: "test.txt", Content: []byte("file content")},
	})
	c.Execute(req) //nolint:errcheck
	rec, _ := srv.TakeRequest(time.Second)
	if !strings.HasPrefix(rec.Headers.Get("Content-Type"), "multipart/form-data") {
		t.Errorf("expected multipart content type, got %q", rec.Headers.Get("Content-Type"))
	}
}

func TestRequest_WithMultipart_CustomContentType(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Post(srv.URL() + "/upload").WithMultipart([]MultipartField{
		{FieldName: "img", FileName: "photo.jpg", ContentType: "image/jpeg", Content: []byte("JPEG")},
	})
	_, err := c.Execute(req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestRequest_WithMultipart_WithReader(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Post(srv.URL() + "/upload").WithMultipart([]MultipartField{
		{FieldName: "data", FileName: "data.bin", Reader: bytes.NewBufferString("streaming content")},
	})
	_, err := c.Execute(req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestRequest_WithMultipart_SanitizesCRLF(t *testing.T) {
	t.Parallel()
	// A FieldName or FileName containing CR or LF would allow injection of
	// arbitrary MIME headers into the multipart body. Verify they are stripped.
	req := New().Post("http://example.com/upload").WithMultipart([]MultipartField{
		{
			FieldName:   "evil\r\nX-Injected: hdr",
			FileName:    "file\r\nX-Injected2: hdr",
			ContentType: "text/plain",
			Content:     []byte("payload"),
		},
	})
	body := string(req.bodyBytes)
	// After stripping CRLF, "X-Injected: hdr" must NOT appear on its own line.
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "X-Injected") {
			t.Errorf("CRLF injection not stripped; injected header found in body: %q", trimmed)
		}
	}
}

func TestRequest_WithMultipart_SanitizesQuotes(t *testing.T) {
	t.Parallel()
	// An embedded double-quote in FieldName or FileName must be escaped so it
	// does not break the Content-Disposition quoted-string.
	req := New().Post("http://example.com/upload").WithMultipart([]MultipartField{
		{
			FieldName:   `na"me`,
			FileName:    `fi"le.txt`,
			ContentType: "text/plain",
			Content:     []byte("payload"),
		},
	})
	body := string(req.bodyBytes)
	// Must NOT contain unescaped quote breaking the parameter: name="na"me"
	if strings.Contains(body, `name="na"me"`) {
		t.Errorf("unescaped double-quote in FieldName; body:\n%s", body)
	}
	// Must contain the properly escaped form: name="na\"me"
	if !strings.Contains(body, `name="na\"me"`) {
		t.Errorf("expected escaped double-quote in Content-Disposition; body:\n%s", body)
	}
}

func TestRequest_WithBearerToken(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Get(srv.URL() + "/").WithBearerToken("mytoken")
	c.Execute(req) //nolint:errcheck
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Headers.Get("Authorization") != "Bearer mytoken" {
		t.Errorf("expected Bearer mytoken, got %q", rec.Headers.Get("Authorization"))
	}
}

func TestRequest_WithBasicAuth(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Get(srv.URL()+"/").WithBasicAuth("user", "pass")
	c.Execute(req) //nolint:errcheck
	rec, _ := srv.TakeRequest(time.Second)
	auth := rec.Headers.Get("Authorization")
	if !strings.HasPrefix(auth, "Basic ") {
		t.Errorf("expected Basic auth header, got %q", auth)
	}
}

func TestRequest_WithIdempotencyKey(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Post(srv.URL() + "/").WithIdempotencyKey("my-key-123")
	c.Execute(req) //nolint:errcheck
	rec, _ := srv.TakeRequest(time.Second)
	if rec.Headers.Get("X-Idempotency-Key") != "my-key-123" {
		t.Errorf("expected X-Idempotency-Key: my-key-123, got %q", rec.Headers.Get("X-Idempotency-Key"))
	}
}

func TestRequest_WithTimeout_Execution(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Delay: 200 * time.Millisecond})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	req := c.Get(srv.URL() + "/slow").WithTimeout(50 * time.Millisecond)
	_, err := c.Execute(req)
	if err == nil {
		t.Error("expected timeout error")
	}
}

// TestSmartURLNormalisation_APIBaseDetection tests that the build() method correctly
// uses smart path selection: RFC 3986 for host-only URLs, safe normalisation for APIs.
func TestSmartURLNormalisation_APIBaseDetection(t *testing.T) {
	t.Parallel()

	// Test 1: Host-only base URL uses RFC 3986 path resolution
	{
		srv := testutil.NewMockServer()
		defer srv.Close()
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

		c := New(
			WithBaseURL(srv.URL()),
			WithDisableRetry(),
			WithDisableCircuitBreaker(),
		)
		_, _ = c.Execute(c.Get("users"))

		rec, _ := srv.TakeRequest(time.Second)
		if rec.Path != "/users" {
			t.Errorf("host-only URL: expected /users, got %q", rec.Path)
		}
	}

	// Test 2: API base URL (/v1) preserves path via safe normalisation
	{
		srv := testutil.NewMockServer()
		defer srv.Close()
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

		c := New(
			WithBaseURL(srv.URL()+"/v1"),
			WithDisableRetry(),
			WithDisableCircuitBreaker(),
		)
		_, _ = c.Execute(c.Get("users"))

		rec, _ := srv.TakeRequest(time.Second)
		if rec.Path != "/v1/users" {
			t.Errorf("API base /v1: expected /v1/users, got %q", rec.Path)
		}
	}

	// Test 3: API base URL (/odata) preserves path via safe normalisation
	{
		srv := testutil.NewMockServer()
		defer srv.Close()
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

		c := New(
			WithBaseURL(srv.URL()+"/odata"),
			WithDisableRetry(),
			WithDisableCircuitBreaker(),
		)
		_, _ = c.Execute(c.Get("Products/$count"))

		rec, _ := srv.TakeRequest(time.Second)
		if rec.Path != "/odata/Products/$count" {
			t.Errorf("OData base: expected /odata/Products/$count, got %q", rec.Path)
		}
	}

	// Test 4: Multi-segment API path is preserved
	{
		srv := testutil.NewMockServer()
		defer srv.Close()
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

		c := New(
			WithBaseURL(srv.URL()+"/company/api"),
			WithDisableRetry(),
			WithDisableCircuitBreaker(),
		)
		_, _ = c.Execute(c.Get("data"))

		rec, _ := srv.TakeRequest(time.Second)
		if rec.Path != "/company/api/data" {
			t.Errorf("Multi-segment path: expected /company/api/data, got %q", rec.Path)
		}
	}

	// Test 5: API base with trailing slash is normalised correctly
	{
		srv := testutil.NewMockServer()
		defer srv.Close()
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

		c := New(
			WithBaseURL(srv.URL()+"/v1/"),
			WithDisableRetry(),
			WithDisableCircuitBreaker(),
		)
		_, _ = c.Execute(c.Get("users"))

		rec, _ := srv.TakeRequest(time.Second)
		if rec.Path != "/v1/users" {
			t.Errorf("API base with trailing slash: expected /v1/users, got %q", rec.Path)
		}
	}

	// Test 6: Relative path with leading slash is normalised correctly
	{
		srv := testutil.NewMockServer()
		defer srv.Close()
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

		c := New(
			WithBaseURL(srv.URL()+"/v1"),
			WithDisableRetry(),
			WithDisableCircuitBreaker(),
		)
		_, _ = c.Execute(c.Get("/users"))

		rec, _ := srv.TakeRequest(time.Second)
		if rec.Path != "/v1/users" {
			t.Errorf("API base with leading slash in relative: expected /v1/users, got %q", rec.Path)
		}
	}

	// Test 7: Both trailing and leading slashes are normalised
	{
		srv := testutil.NewMockServer()
		defer srv.Close()
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

		c := New(
			WithBaseURL(srv.URL()+"/v1/"),
			WithDisableRetry(),
			WithDisableCircuitBreaker(),
		)
		_, _ = c.Execute(c.Get("/users"))

		rec, _ := srv.TakeRequest(time.Second)
		if rec.Path != "/v1/users" {
			t.Errorf("Both trailing/leading slashes: expected /v1/users, got %q", rec.Path)
		}
	}

	// Test 8: Host-only base URL + double-slash path must NOT produce //path.
	// Regression: When the entity-set name has a leading slash (e.g. "/Products")
	// traverse prepends another slash in buildURL, yielding "//Products". The
	// RFC3986 fast path must normalise that to "/Products" so servers that
	// treat "//path" differently (SAP, nginx) do not return 401/404.
	{
		srv := testutil.NewMockServer()
		defer srv.Close()
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

		c := New(
			WithBaseURL(srv.URL()),
			WithDisableRetry(),
			WithDisableCircuitBreaker(),
		)
		_, _ = c.Execute(c.Get("//Products"))

		rec, _ := srv.TakeRequest(time.Second)
		if rec.Path != "/Products" {
			t.Errorf("double-slash path normalisation: expected /Products, got %q", rec.Path)
		}
	}
}

// TestSmartURLNormalisation_ConsistencyAcrossRetries ensures that URL caching
// produces the same URL regardless of whether the request is retried.
func TestSmartURLNormalisation_ConsistencyAcrossRetries(t *testing.T) {
	t.Parallel()

	srv := testutil.NewMockServer()
	defer srv.Close()

	// Enqueue two responses: first fails, second succeeds
	srv.Enqueue(testutil.MockResponse{Status: http.StatusInternalServerError})
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(
		WithBaseURL(srv.URL()+"/v1"),
		WithRetry(&RetryConfig{
			MaxAttempts:     2,
			InitialInterval: 1 * time.Millisecond,
			MaxInterval:     1 * time.Millisecond,
			RetryableStatus: []int{http.StatusInternalServerError},
		}),
		WithDisableCircuitBreaker(),
	)

	_, _ = c.Execute(c.Get("users"))

	// Verify both requests went to the same path
	rec1, _ := srv.TakeRequest(time.Second)
	rec2, _ := srv.TakeRequest(time.Second)

	if rec1.Path != "/v1/users" {
		t.Errorf("first request: expected /v1/users, got %q", rec1.Path)
	}
	if rec2.Path != "/v1/users" {
		t.Errorf("retry request: expected /v1/users, got %q", rec2.Path)
	}
	if rec1.Path != rec2.Path {
		t.Errorf("retry consistency: first=%q, retry=%q", rec1.Path, rec2.Path)
	}
}

// TestURLNormalisationMode_ExplicitRFC3986 tests forcing RFC 3986 normalisation.
func TestURLNormalisationMode_ExplicitRFC3986(t *testing.T) {
	t.Parallel()

	// With explicit RFC3986 mode, even API URLs use zero-alloc resolution
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(
		WithBaseURL(srv.URL()),
		WithURLNormalisation(NormalisationRFC3986),
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
	)

	_, _ = c.Execute(c.Get("/users"))

	rec, _ := srv.TakeRequest(time.Second)
	if rec.Path != "/users" {
		t.Errorf("RFC3986 mode: expected /users, got %q", rec.Path)
	}
}

// TestURLNormalisationMode_ExplicitAPI tests forcing safe normalisation for all URLs.
func TestURLNormalisationMode_ExplicitAPI(t *testing.T) {
	t.Parallel()

	// With explicit API mode, even simple URLs use safe normalisation
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(
		WithBaseURL(srv.URL()),
		WithURLNormalisation(NormalisationAPI),
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
	)

	_, _ = c.Execute(c.Get("users"))

	rec, _ := srv.TakeRequest(time.Second)
	if rec.Path != "/users" {
		t.Errorf("API mode with host-only URL: expected /users, got %q", rec.Path)
	}
}

// TestURLNormalisationMode_ExplicitAPI_WithPath tests safe normalisation preserves paths.
func TestURLNormalisationMode_ExplicitAPI_WithPath(t *testing.T) {
	t.Parallel()

	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(
		WithBaseURL(srv.URL()+"/v1"),
		WithURLNormalisation(NormalisationAPI),
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
	)

	_, _ = c.Execute(c.Get("users"))

	rec, _ := srv.TakeRequest(time.Second)
	if rec.Path != "/v1/users" {
		t.Errorf("API mode with base path: expected /v1/users, got %q", rec.Path)
	}
}

// TestURLNormalisationMode_Default tests that Auto mode is default.
func TestURLNormalisationMode_Default(t *testing.T) {
	t.Parallel()

	c := New()
	if c.config.URLNormalisationMode != NormalisationAuto {
		t.Errorf("expected default mode to be NormalisationAuto (%d), got %d",
			NormalisationAuto, c.config.URLNormalisationMode)
	}
}

// FuzzPathParams tests that path parameter substitution does not panic
// on malformed keys or values.

func FuzzPathParams(f *testing.F) {
	f.Add("key", "value")
	f.Add("{key}", "value/with/slash")
	f.Add("", "")

	f.Fuzz(func(t *testing.T, key, value string) {
		req := newRequest("GET", "/{"+key+"}")
		req.WithPathParam(key, value)
		_ = req.applyPathParams(req.rawURL)
	})
}
