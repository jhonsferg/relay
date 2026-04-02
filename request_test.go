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

// TestSmartURLNormalization_APIBaseDetection tests that the build() method correctly
// uses smart path selection: RFC 3986 for host-only URLs, safe normalization for APIs.
func TestSmartURLNormalization_APIBaseDetection(t *testing.T) {
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

	// Test 2: API base URL (/v1) preserves path via safe normalization
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

	// Test 3: API base URL (/odata) preserves path via safe normalization
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

	// Test 5: API base with trailing slash is normalized correctly
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

	// Test 6: Relative path with leading slash is normalized correctly
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

	// Test 7: Both trailing and leading slashes are normalized
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
}

// TestSmartURLNormalization_ConsistencyAcrossRetries ensures that URL caching
// produces the same URL regardless of whether the request is retried.
func TestSmartURLNormalization_ConsistencyAcrossRetries(t *testing.T) {
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

// TestURLNormalizationMode_ExplicitRFC3986 tests forcing RFC 3986 normalization.
func TestURLNormalizationMode_ExplicitRFC3986(t *testing.T) {
	t.Parallel()

	// With explicit RFC3986 mode, even API URLs use zero-alloc resolution
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(
		WithBaseURL(srv.URL()),
		WithURLNormalization(NormalizationRFC3986),
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
	)

	_, _ = c.Execute(c.Get("/users"))

	rec, _ := srv.TakeRequest(time.Second)
	if rec.Path != "/users" {
		t.Errorf("RFC3986 mode: expected /users, got %q", rec.Path)
	}
}

// TestURLNormalizationMode_ExplicitAPI tests forcing safe normalization for all URLs.
func TestURLNormalizationMode_ExplicitAPI(t *testing.T) {
	t.Parallel()

	// With explicit API mode, even simple URLs use safe normalization
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(
		WithBaseURL(srv.URL()),
		WithURLNormalization(NormalizationAPI),
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
	)

	_, _ = c.Execute(c.Get("users"))

	rec, _ := srv.TakeRequest(time.Second)
	if rec.Path != "/users" {
		t.Errorf("API mode with host-only URL: expected /users, got %q", rec.Path)
	}
}

// TestURLNormalizationMode_ExplicitAPI_WithPath tests safe normalization preserves paths.
func TestURLNormalizationMode_ExplicitAPI_WithPath(t *testing.T) {
	t.Parallel()

	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(
		WithBaseURL(srv.URL()+"/v1"),
		WithURLNormalization(NormalizationAPI),
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
	)

	_, _ = c.Execute(c.Get("users"))

	rec, _ := srv.TakeRequest(time.Second)
	if rec.Path != "/v1/users" {
		t.Errorf("API mode with base path: expected /v1/users, got %q", rec.Path)
	}
}

// TestURLNormalizationMode_Default tests that Auto mode is default.
func TestURLNormalizationMode_Default(t *testing.T) {
	t.Parallel()

	c := New()
	if c.config.URLNormalizationMode != NormalizationAuto {
		t.Errorf("expected default mode to be NormalizationAuto (%d), got %d",
			NormalizationAuto, c.config.URLNormalizationMode)
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
