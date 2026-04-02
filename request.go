package relay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"

	"github.com/jhonsferg/relay/internal/pool"
)

// MultipartField represents a single part in a multipart/form-data request.
// Set FileName to create a file part; leave it empty for a plain form field.
// Reader takes precedence over Content when both are set.
// ContentType overrides the default application/octet-stream for file parts.
type MultipartField struct {
	// FieldName is the form field name (the name attribute in HTML).
	FieldName string

	// FileName is the filename reported to the server. A non-empty value
	// creates a file part; an empty value creates a plain form field.
	FileName string

	// ContentType is an optional MIME type for file parts.
	// When empty, multipart.Writer uses application/octet-stream.
	ContentType string

	// Content holds in-memory file or field data. Use Reader instead for
	// streaming sources such as os.File to avoid loading the whole file.
	Content []byte

	// Reader is a streaming data source. Takes precedence over Content when
	// both are non-nil. The caller is responsible for closing it after Execute.
	Reader io.Reader
}

// Request is a fluent builder for a single HTTP call. All With* methods return
// the receiver so they can be chained without intermediate variables.
//
// A Request must not be shared between goroutines after it is passed to
// [Client.Execute].
type Request struct {
	// method is the HTTP verb (GET, POST, …).
	method string

	// rawURL is the URL as provided by the caller. It may be a full URL or a
	// path relative to [Config.BaseURL].
	rawURL string

	// headers are per-request HTTP headers. They take precedence over
	// [Config.DefaultHeaders].
	headers map[string]string

	// query holds URL query parameters accumulated via WithQueryParam* methods.
	query url.Values

	// bodyBytes is the serialised request body. Nil means no body.
	bodyBytes []byte

	// ctx is the context governing cancellation, deadline, and value propagation.
	ctx context.Context

	// timeout is the per-request deadline applied on top of ctx. When > 0,
	// Execute wraps ctx with context.WithTimeout before sending.
	timeout time.Duration

	// pathParams holds {placeholder} → value substitutions applied to rawURL
	// before the request is built.
	pathParams map[string]string

	// tags are client-side key/value labels attached by the caller. They are
	// never sent as HTTP headers; they are visible to OnBeforeRequest and
	// OnAfterResponse hooks.
	tags map[string]string

	// uploadProgress is called during body upload with bytes transferred / total.
	uploadProgress ProgressFunc

	// downloadProgress is called during body download with bytes transferred / total.
	downloadProgress ProgressFunc

	// idempotencyKey is the X-Idempotency-Key header value to use. When set,
	// it is reused across retry attempts. Set via WithIdempotencyKey or
	// auto-generated when WithAutoIdempotencyKey is configured.
	idempotencyKey string

	// maxBodyBytes is a per-request override for [Config.MaxResponseBodyBytes].
	// Zero means use the client-level default.
	maxBodyBytes int64

	// pooledReader is a reference to a pooled bytes.Reader if one was created
	// during build(). It must be returned to the pool after the request is sent.
	pooledReader *bytes.Reader

	// builtURL caches the last-built URL string to avoid rebuilding on retries
	// when path/query params haven't changed. Updated when build() is called.
	builtURL string

	// urlDirty is set to true when path params or query params are modified,
	// invalidating builtURL cache. Check before skipping URL rebuild.
	urlDirty bool

	// encodedQuery caches the result of r.query.Encode() to avoid re-encoding
	// when only path params changed. Populated on first build; reused when
	// queryDirty is false.
	encodedQuery string

	// queryDirty is set to true by WithQueryParam* methods and cleared after
	// re-encoding. When false and encodedQuery is non-empty, the cached encoded
	// query is reused without calling r.query.Encode() again.
	queryDirty bool
}

// newRequest allocates a Request with all maps initialised and a background
// context. It is the single construction point; callers never create Request
// literals directly.
func newRequest(method, rawURL string) *Request {
	return &Request{
		method:     method,
		rawURL:     rawURL,
		headers:    make(map[string]string),
		query:      url.Values{},
		ctx:        context.Background(),
		pathParams: make(map[string]string),
	}
}

// Method returns the HTTP verb for this request (e.g. "GET", "POST").
func (r *Request) Method() string { return r.method }

// URL returns the raw URL string as provided to the builder, before path
// parameters are substituted or query parameters are appended.
func (r *Request) URL() string { return r.rawURL }

// WithContext sets the context used for this request. If the context carries a
// deadline it races with any timeout set via [Request.WithTimeout] - whichever
// fires first cancels the request.
func (r *Request) WithContext(ctx context.Context) *Request { r.ctx = ctx; return r }

// WithTimeout sets a per-request timeout that wraps the existing context.
// When the timeout fires, [Client.Execute] returns [ErrTimeout].
func (r *Request) WithTimeout(d time.Duration) *Request { r.timeout = d; return r }

// WithPathParam replaces a {key} placeholder in the URL template before
// sending. The value is percent-encoded automatically.
//
//	client.Get("/users/{id}").WithPathParam("id", "usr_42")
//	// → GET /users/usr_42
func (r *Request) WithPathParam(key, value string) *Request {
	r.pathParams[key] = value
	r.urlDirty = true
	return r
}

// WithPathParams sets multiple URL path parameters at once.
//
//	client.Get("/orgs/{org}/users/{id}").WithPathParams(map[string]string{
//	    "org": "alicorp",
//	    "id":  "usr_42",
//	})
func (r *Request) WithPathParams(params map[string]string) *Request {
	for k, v := range params {
		r.pathParams[k] = v
	}
	r.urlDirty = true
	return r
}

// WithTag attaches a client-side key/value label to the request.
// Tags are NOT sent as HTTP headers - they are visible to [Config.OnBeforeRequest]
// and [Config.OnAfterResponse] hooks for logging, metrics labelling, etc.
//
//	req.WithTag("operation", "CreateOrder").WithTag("team", "payments")
func (r *Request) WithTag(key, value string) *Request {
	if r.tags == nil {
		r.tags = make(map[string]string)
	}
	r.tags[key] = value
	return r
}

// Tag returns the value of a tag previously set via [Request.WithTag], or ""
// if the tag is absent.
func (r *Request) Tag(key string) string { return r.tags[key] }

// Tags returns a copy of all tags attached to this request. Returns nil if no
// tags have been set.
func (r *Request) Tags() map[string]string {
	if len(r.tags) == 0 {
		return nil
	}
	cp := make(map[string]string, len(r.tags))
	for k, v := range r.tags {
		cp[k] = v
	}
	return cp
}

// WithHeader sets (or replaces) a single request header. Per-request headers
// take precedence over [Config.DefaultHeaders].
func (r *Request) WithHeader(key, value string) *Request {
	r.headers[key] = value
	return r
}

// WithHeaders merges the given map into the request headers. Later keys in the
// map override earlier ones; per-request headers always beat defaults.
func (r *Request) WithHeaders(headers map[string]string) *Request {
	for k, v := range headers {
		r.headers[k] = v
	}
	return r
}

// WithQueryParam sets (or replaces) a single URL query parameter.
func (r *Request) WithQueryParam(key, value string) *Request {
	r.query.Set(key, value)
	r.urlDirty = true
	r.queryDirty = true
	return r
}

// WithQueryParams merges the given map into the URL query string. Later keys
// override earlier ones for the same name.
func (r *Request) WithQueryParams(params map[string]string) *Request {
	for k, v := range params {
		r.query.Set(k, v)
	}
	r.urlDirty = true
	r.queryDirty = true
	return r
}

// WithQueryParamValues sets a multi-value query parameter, replacing any
// previously set values for the same key.
//
//	req.WithQueryParamValues("ids", []string{"1", "2", "3"})
//	// → ?ids=1&ids=2&ids=3
func (r *Request) WithQueryParamValues(key string, values []string) *Request {
	r.query[key] = values
	r.urlDirty = true
	r.queryDirty = true
	return r
}

// WithBody sets the raw request body bytes. The caller is responsible for also
// setting Content-Type via [Request.WithContentType].
func (r *Request) WithBody(body []byte) *Request { r.bodyBytes = body; return r }

// WithContentType sets the Content-Type request header.
func (r *Request) WithContentType(ct string) *Request { r.headers["Content-Type"] = ct; return r }

// WithAccept sets the Accept request header.
func (r *Request) WithAccept(accept string) *Request { r.headers["Accept"] = accept; return r }

// WithUserAgent sets the User-Agent request header, overriding any client-level
// default set via [WithDefaultHeaders].
func (r *Request) WithUserAgent(ua string) *Request { r.headers["User-Agent"] = ua; return r }

// WithRequestID sets the X-Request-Id header. Useful for distributed tracing
// and log correlation when managing request identifiers outside of OTel.
func (r *Request) WithRequestID(id string) *Request { r.headers["X-Request-Id"] = id; return r }

// WithAPIKey sets a header-based API key. The header name varies by service;
// common choices are "X-API-Key" and "Authorization".
//
//	req.WithAPIKey("X-API-Key", os.Getenv("SERVICE_API_KEY"))
func (r *Request) WithAPIKey(headerName, apiKey string) *Request {
	r.headers[headerName] = apiKey
	return r
}

// WithBodyReader reads all bytes from reader and sets them as the request body.
// If the reader returns an error the body is left unchanged. For very large
// payloads prefer [Client.ExecuteStream] combined with a custom RoundTripper.
func (r *Request) WithBodyReader(reader io.Reader) *Request {
	data, err := io.ReadAll(reader)
	if err != nil {
		return r
	}
	r.bodyBytes = data
	return r
}

// WithJSON marshals v to JSON, sets the body, and sets Content-Type to
// application/json. If marshalling fails the body is left unchanged.
func (r *Request) WithJSON(v interface{}) *Request {
	data, err := json.Marshal(v)
	if err != nil {
		return r
	}
	r.bodyBytes = data
	r.headers["Content-Type"] = "application/json"
	return r
}

// WithFormData URL-encodes data and sets Content-Type to
// application/x-www-form-urlencoded.
func (r *Request) WithFormData(data map[string]string) *Request {
	form := url.Values{}
	for k, v := range data {
		form.Set(k, v)
	}
	r.bodyBytes = []byte(form.Encode())
	r.headers["Content-Type"] = "application/x-www-form-urlencoded"
	return r
}

// WithMultipart builds a multipart/form-data body from the provided fields.
// Supports plain form fields and file uploads with optional Content-Type
// overrides.
func (r *Request) WithMultipart(fields []MultipartField) *Request {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, f := range fields {
		if f.FileName != "" {
			var part io.Writer
			if f.ContentType != "" {
				h := make(textproto.MIMEHeader)
				h.Set("Content-Disposition", fmt.Sprintf(
					`form-data; name="%s"; filename="%s"`, f.FieldName, f.FileName,
				))
				h.Set("Content-Type", f.ContentType)
				part, _ = w.CreatePart(h)
			} else {
				part, _ = w.CreateFormFile(f.FieldName, f.FileName)
			}
			if f.Reader != nil {
				_, _ = io.Copy(part, f.Reader)
			} else {
				_, _ = part.Write(f.Content)
			}
		} else {
			_ = w.WriteField(f.FieldName, string(f.Content))
		}
	}
	_ = w.Close()
	r.bodyBytes = buf.Bytes()
	r.headers["Content-Type"] = w.FormDataContentType()
	return r
}

// WithBearerToken sets the Authorization header to "Bearer <token>".
func (r *Request) WithBearerToken(token string) *Request {
	r.headers["Authorization"] = "Bearer " + token
	return r
}

// WithBasicAuth sets the Authorization header to the RFC 7617 Basic credential
// for the given username and password.
func (r *Request) WithBasicAuth(username, password string) *Request {
	credentials := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	r.headers["Authorization"] = "Basic " + credentials
	return r
}

// WithUploadProgress registers a callback that is invoked periodically during
// request body upload. transferred is the number of bytes sent so far; total is
// the body size or -1 if unknown.
func (r *Request) WithUploadProgress(fn ProgressFunc) *Request {
	r.uploadProgress = fn
	return r
}

// WithDownloadProgress registers a callback that is invoked periodically during
// response body download. transferred is bytes read so far; total is Content-Length
// or -1 if the header is absent.
func (r *Request) WithDownloadProgress(fn ProgressFunc) *Request {
	r.downloadProgress = fn
	return r
}

// WithIdempotencyKey sets a custom X-Idempotency-Key header value. The key is
// reused unchanged across all retry attempts for this request.
func (r *Request) WithIdempotencyKey(key string) *Request {
	r.idempotencyKey = key
	return r
}

// WithMaxBodySize overrides the client-level [Config.MaxResponseBodyBytes] for
// this single request. Pass 0 to fall back to the client default (10 MB).
// Pass -1 to remove the limit entirely for this request.
func (r *Request) WithMaxBodySize(n int64) *Request { r.maxBodyBytes = n; return r }

// Clone returns a deep copy of the request. All maps and slices are
// independently duplicated so mutations to the clone do not affect the
// original, and vice-versa. The body bytes slice is also copied.
//
// Clone is useful when the same base request needs to be dispatched with
// different headers, query params, or bodies without constructing from scratch.
func (r *Request) Clone() *Request {
	clone := *r

	clone.headers = make(map[string]string, len(r.headers))
	for k, v := range r.headers {
		clone.headers[k] = v
	}

	clone.query = make(url.Values, len(r.query))
	for k, v := range r.query {
		clone.query[k] = append([]string(nil), v...)
	}

	clone.pathParams = make(map[string]string, len(r.pathParams))
	for k, v := range r.pathParams {
		clone.pathParams[k] = v
	}

	if r.tags != nil {
		clone.tags = make(map[string]string, len(r.tags))
		for k, v := range r.tags {
			clone.tags[k] = v
		}
	}

	if r.bodyBytes != nil {
		clone.bodyBytes = append([]byte(nil), r.bodyBytes...)
	}

	// pooledReader must not be cloned - each request build creates its own
	clone.pooledReader = nil

	// URL cache must be invalidated for clone since params may change.
	// Preserve encodedQuery so the clone can reuse it if query is unchanged.
	clone.builtURL = ""
	clone.urlDirty = false

	return &clone
}

// withCtx returns a shallow clone of r with the context replaced. Used
// internally by [Client.Execute] when applying a per-request timeout so the
// original Request is not mutated.
func (r *Request) withCtx(ctx context.Context) *Request {
	clone := *r
	clone.ctx = ctx
	return &clone
}

// applyPathParams substitutes every {key} placeholder in rawURL with its
// corresponding percent-encoded value from pathParams.
func (r *Request) applyPathParams(rawURL string) string {
	if len(r.pathParams) == 0 {
		return rawURL
	}

	// Build placeholders map to avoid allocating "{key}" string in each iteration.
	result := rawURL
	for k, v := range r.pathParams {
		placeholder := "{" + k + "}"
		result = strings.ReplaceAll(result, placeholder, url.PathEscape(v))
	}
	return result
}

// build constructs the stdlib *http.Request from this builder's state.
// It applies path params, resolves the URL against baseURL/parsedBaseURL,
// appends query params, and sets all headers. parsedBaseURL, if non-nil,
// is used as an optimization to avoid re-parsing. Built URL is cached to
// avoid rebuild on retries when params haven't changed.
//
// normalizationMode controls which URL resolution strategy is used:
//   - NormalizationAuto: Intelligent detection (API vs host-only)
//   - NormalizationRFC3986: Force RFC 3986 (zero-alloc, breaks APIs)
//   - NormalizationAPI: Force safe normalization (preserves paths)
func (r *Request) build(baseURL string, parsedBaseURL *url.URL, normalizationMode URLNormalizationMode) (*http.Request, error) {
	// Fast path: if URL hasn't been modified and was cached, reuse it
	if r.builtURL != "" && !r.urlDirty {
		// Reuse cached URL for retries
		var bodyReader io.Reader
		if len(r.bodyBytes) > 0 {
			r.pooledReader = pool.GetBytesReader(r.bodyBytes)
			bodyReader = r.pooledReader
			if r.uploadProgress != nil {
				bodyReader = newProgressReader(bodyReader, int64(len(r.bodyBytes)), r.uploadProgress)
			}
		}
		req, err := http.NewRequestWithContext(r.ctx, r.method, r.builtURL, bodyReader)
		if err != nil {
			return nil, err
		}
		for k, v := range r.headers {
			req.Header.Set(k, v)
		}
		return req, nil
	}

	fullURL := r.applyPathParams(r.rawURL)
	if baseURL != "" && !strings.HasPrefix(fullURL, "http://") && !strings.HasPrefix(fullURL, "https://") {
		// Determine which normalization strategy to use
		useRFC3986 := false
		switch normalizationMode {
		case NormalizationAuto:
			// Intelligent detection: RFC 3986 for host-only, safe for APIs
			useRFC3986 = parsedBaseURL != nil && !isAPIBase(baseURL)
		case NormalizationRFC3986:
			// Force RFC 3986 (requires parsed URL)
			useRFC3986 = parsedBaseURL != nil
		case NormalizationAPI:
			// Force safe normalization
			useRFC3986 = false
		}

		if useRFC3986 {
			// Path 1: Use pre-parsed URL for RFC 3986 resolution (host-only URLs).
			// Zero allocations; reuses parsed URL. Safe for URLs without path components.
			resolved := parsedBaseURL.ResolveReference(&url.URL{Path: fullURL})
			fullURL = resolved.String()
		} else {
			// Path 2: Use safe string normalization for API URLs.
			// Handles API base URLs with path components (e.g., http://api.com/v1/odata)
			// correctly by preserving the entire base path instead of replacing it per RFC 3986.
			var sb strings.Builder
			sb.Grow(len(baseURL) + len(fullURL) + 1)
			// TrimRight baseURL and write
			trimmedBase := strings.TrimRight(baseURL, "/")
			sb.WriteString(trimmedBase)
			sb.WriteByte('/')
			// TrimLeft fullURL and write
			trimmedPath := strings.TrimLeft(fullURL, "/")
			sb.WriteString(trimmedPath)
			fullURL = sb.String()
		}
	}
	if len(r.query) > 0 {
		if !strings.Contains(fullURL, "?") {
			// Fast path: no existing query params — skip url.Parse and
			// use cached encoded query when params haven't changed.
			if r.queryDirty || r.encodedQuery == "" {
				r.encodedQuery = r.query.Encode()
				r.queryDirty = false
			}
			var sb strings.Builder
			sb.Grow(len(fullURL) + 1 + len(r.encodedQuery))
			sb.WriteString(fullURL)
			sb.WriteByte('?')
			sb.WriteString(r.encodedQuery)
			fullURL = sb.String()
		} else {
			// Slow path: URL already contains a query string — merge.
			parsed, err := url.Parse(fullURL)
			if err != nil {
				return nil, err
			}
			existing := parsed.Query()
			for k, vs := range r.query {
				for _, v := range vs {
					existing.Add(k, v)
				}
			}
			parsed.RawQuery = existing.Encode()
			fullURL = parsed.String()
		}
	}

	// Cache the built URL and clear dirty flag for next build
	r.builtURL = fullURL
	r.urlDirty = false

	var bodyReader io.Reader
	if len(r.bodyBytes) > 0 {
		r.pooledReader = pool.GetBytesReader(r.bodyBytes)
		bodyReader = r.pooledReader
		if r.uploadProgress != nil {
			bodyReader = newProgressReader(bodyReader, int64(len(r.bodyBytes)), r.uploadProgress)
		}
	}
	req, err := http.NewRequestWithContext(r.ctx, r.method, fullURL, bodyReader)
	if err != nil {
		return nil, err
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// releasePooledReader returns the pooled bytes.Reader to the pool if one was
// created during build(). Must be called after the request has been sent.
func (r *Request) releasePooledReader() {
	if r.pooledReader != nil {
		pool.PutBytesReader(r.pooledReader)
		r.pooledReader = nil
	}
}
