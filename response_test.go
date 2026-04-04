package relay

import (
	"encoding/xml"
	"net/http"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

func makeResponse(t *testing.T, status int, body string, headers map[string]string) *Response {
	t.Helper()
	srv := testutil.NewMockServer()
	t.Cleanup(srv.Close)
	resp := testutil.MockResponse{Status: status, Body: body}
	if headers != nil {
		resp.Headers = headers
	}
	srv.Enqueue(resp)
	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	r, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return r
}

func TestResponse_BodyReader(t *testing.T) {
	t.Parallel()
	resp := makeResponse(t, 200, "read me", nil)
	buf := make([]byte, 7)
	n, _ := resp.BodyReader().Read(buf)
	if string(buf[:n]) != "read me" {
		t.Errorf("BodyReader: got %q", string(buf[:n]))
	}
}

func TestResponse_IsTruncated_False(t *testing.T) {
	t.Parallel()
	resp := makeResponse(t, 200, "short", nil)
	if resp.IsTruncated() {
		t.Error("expected IsTruncated=false for short body")
	}
}

func TestResponse_IsTruncated_True(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: 200, Body: "1234567890"})
	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithMaxResponseBodyBytes(5))
	r, _ := c.Execute(c.Get(srv.URL() + "/"))
	if !r.IsTruncated() {
		t.Error("expected IsTruncated=true when body exceeds limit")
	}
	if string(r.Body()) != "12345" {
		t.Errorf("expected truncated body '12345', got %q", string(r.Body()))
	}
}

func TestResponse_WasRedirected_False(t *testing.T) {
	t.Parallel()
	resp := makeResponse(t, 200, "", nil)
	if resp.WasRedirected() {
		t.Error("expected WasRedirected=false for direct response")
	}
}

func TestResponse_ContentType(t *testing.T) {
	t.Parallel()
	resp := makeResponse(t, 200, "", map[string]string{"Content-Type": "text/html; charset=utf-8"})
	if resp.ContentType() != "text/html; charset=utf-8" {
		t.Errorf("expected text/html, got %q", resp.ContentType())
	}
}

func TestResponse_Header(t *testing.T) {
	t.Parallel()
	resp := makeResponse(t, 200, "", map[string]string{"X-Custom": "relay"})
	if resp.Header("X-Custom") != "relay" {
		t.Errorf("expected relay, got %q", resp.Header("X-Custom"))
	}
}

func TestResponse_Location(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Location": "https://other.example.com/path"},
	})
	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	resp, _ := c.Execute(c.Get(srv.URL() + "/"))
	if resp.Location() != "https://other.example.com/path" {
		t.Errorf("expected location, got %q", resp.Location())
	}
}

func TestResponse_Location_Empty(t *testing.T) {
	t.Parallel()
	resp := makeResponse(t, 200, "", nil)
	if resp.Location() != "" {
		t.Errorf("expected empty location, got %q", resp.Location())
	}
}

func TestResponse_Raw(t *testing.T) {
	t.Parallel()
	resp := makeResponse(t, 200, "body", nil)
	if resp.Raw() == nil {
		t.Error("expected non-nil Raw()")
	}
}

func TestResponse_Cookies(t *testing.T) {
	t.Parallel()
	resp := makeResponse(t, 200, "", map[string]string{"Set-Cookie": "session=abc; Path=/"})
	cookies := resp.Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "session" && c.Value == "abc" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected session cookie, got %v", cookies)
	}
}

func TestResponse_XML(t *testing.T) {
	t.Parallel()
	type Item struct {
		XMLName xml.Name `xml:"item"`
		Name    string   `xml:"name"`
	}
	resp := makeResponse(t, 200, "<item><name>relay</name></item>", nil)
	var item Item
	if err := resp.XML(&item); err != nil {
		t.Fatalf("XML: %v", err)
	}
	if item.Name != "relay" {
		t.Errorf("expected name=relay, got %q", item.Name)
	}
}

func TestResponse_IsRedirect(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code     int
		redirect bool
	}{
		{200, false},
		{301, true},
		{302, true},
		{399, true},
		{400, false},
	}
	for _, tc := range cases {
		resp := &Response{StatusCode: tc.code}
		if resp.IsRedirect() != tc.redirect {
			t.Errorf("status %d: expected IsRedirect=%v", tc.code, tc.redirect)
		}
	}
}

func TestResponse_IsError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code    int
		isError bool
	}{
		{200, false},
		{399, false},
		{400, true},
		{500, true},
	}
	for _, tc := range cases {
		resp := &Response{StatusCode: tc.code}
		if resp.IsError() != tc.isError {
			t.Errorf("status %d: expected IsError=%v", tc.code, tc.isError)
		}
	}
}

func TestResponse_AsHTTPError_ErrorResponse(t *testing.T) {
	t.Parallel()
	resp := &Response{StatusCode: 404, Status: "404 Not Found", body: []byte("not found")}
	httpErr := resp.AsHTTPError()
	if httpErr == nil {
		t.Fatal("expected non-nil HTTPError for 404")
	}
	if httpErr.StatusCode != 404 {
		t.Errorf("expected 404, got %d", httpErr.StatusCode)
	}
}

func TestResponse_AsHTTPError_SuccessResponse(t *testing.T) {
	t.Parallel()
	resp := &Response{StatusCode: 200}
	if resp.AsHTTPError() != nil {
		t.Error("expected nil HTTPError for 200")
	}
}

func TestResponse_IsClientError(t *testing.T) {
	t.Parallel()
	if !(&Response{StatusCode: 400}).IsClientError() {
		t.Error("400 should be client error")
	}
	if !(&Response{StatusCode: 404}).IsClientError() {
		t.Error("404 should be client error")
	}
	if (&Response{StatusCode: 500}).IsClientError() {
		t.Error("500 should NOT be client error")
	}
}

func TestResponse_IsServerError(t *testing.T) {
	t.Parallel()
	if !(&Response{StatusCode: 500}).IsServerError() {
		t.Error("500 should be server error")
	}
	if (&Response{StatusCode: 404}).IsServerError() {
		t.Error("404 should NOT be server error")
	}
}

func TestResponse_IsSuccess(t *testing.T) {
	t.Parallel()
	for _, code := range []int{200, 201, 204, 299} {
		if !(&Response{StatusCode: code}).IsSuccess() {
			t.Errorf("%d should be success", code)
		}
	}
	if (&Response{StatusCode: 300}).IsSuccess() {
		t.Error("300 should NOT be success")
	}
}

// TestClient_Execute_WithRequestTimeout_Cancels verifies per-request timeout.
func TestClient_Execute_WithRequestTimeout_Cancels(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Delay: 300 * time.Millisecond})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker(), WithTimeout(50*time.Millisecond))
	_, err := c.Execute(c.Get(srv.URL() + "/slow"))
	if err == nil {
		t.Error("expected error for timed-out request")
	}
}

// TestResponse_Decode_NoDecoder_JSON verifies Decode falls back to JSON when no
// ResponseDecoder is configured and the content type is application/json.
func TestResponse_Decode_NoDecoder_JSON(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    `{"id":1,"name":"relay"}`,
	})

	type Payload struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer PutResponse(resp)

	var p Payload
	if err = resp.Decode(&p); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if p.ID != 1 || p.Name != "relay" {
		t.Errorf("got %+v, want {1 relay}", p)
	}
}

// TestResponse_Decode_NoDecoder_XML verifies Decode falls back to XML when no
// ResponseDecoder is configured and the content type contains xml.
func TestResponse_Decode_NoDecoder_XML(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/xml"},
		Body:    `<payload><id>2</id><name>relay</name></payload>`,
	})

	type Payload struct {
		XMLName struct{} `xml:"payload"`
		ID      int      `xml:"id"`
		Name    string   `xml:"name"`
	}

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer PutResponse(resp)

	var p Payload
	if err = resp.Decode(&p); err != nil {
		t.Fatalf("Decode XML: %v", err)
	}
	if p.ID != 2 || p.Name != "relay" {
		t.Errorf("got %+v, want {2 relay}", p)
	}
}

// TestResponse_Decode_CustomDecoder verifies that WithResponseDecoder replaces
// the default JSON/XML deserialiser.
func TestResponse_Decode_CustomDecoder(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/x-custom"},
		Body:    "custom:42",
	})

	type Payload struct{ Value int }

	called := false
	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithResponseDecoder(func(ct string, body []byte, v any) error {
			called = true
			if ct != "application/x-custom" {
				t.Errorf("contentType = %q, want application/x-custom", ct)
			}
			p, ok := v.(*Payload)
			if !ok {
				t.Errorf("v is %T, want *Payload", v)
				return nil
			}
			p.Value = 42
			return nil
		}),
	)

	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer PutResponse(resp)

	var p Payload
	if err = resp.Decode(&p); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !called {
		t.Error("custom decoder was not called")
	}
	if p.Value != 42 {
		t.Errorf("Value = %d, want 42", p.Value)
	}
}

// TestResponse_Decode_CustomDecoder_Error verifies that a non-nil error from
// ResponseDecoder propagates from Decode.
func TestResponse_Decode_CustomDecoder_Error(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    `{}`,
	})

	decodeErr := &xml.SyntaxError{Msg: "test error"}
	c := New(
		WithDisableRetry(),
		WithDisableCircuitBreaker(),
		WithResponseDecoder(func(_ string, _ []byte, _ any) error {
			return decodeErr
		}),
	)

	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer PutResponse(resp)

	var v any
	if err = resp.Decode(&v); err != decodeErr {
		t.Errorf("Decode error = %v, want %v", err, decodeErr)
	}
}

// TestResponse_Decode_PoolReuse verifies that the decode field is cleared when
// a Response is returned to the pool and reused.
func TestResponse_Decode_PoolReuse(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    `{"id":1}`,
	})
	srv.Enqueue(testutil.MockResponse{
		Status:  http.StatusOK,
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    `{"id":2}`,
	})

	type P struct{ ID int }

	// First request with a custom decoder.
	c1 := New(
		WithDisableRetry(), WithDisableCircuitBreaker(),
		WithResponseDecoder(func(_ string, _ []byte, v any) error {
			v.(*P).ID = 99 // sentinel
			return nil
		}),
	)
	resp1, _ := c1.Execute(c1.Get(srv.URL() + "/"))
	PutResponse(resp1) // return to pool

	// Second request without a decoder - must NOT use c1's decoder.
	c2 := New(WithDisableRetry(), WithDisableCircuitBreaker())
	resp2, err := c2.Execute(c2.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	defer PutResponse(resp2)

	var p P
	if err = resp2.Decode(&p); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if p.ID == 99 {
		t.Error("pooled response leaked decode func from previous client")
	}
}
