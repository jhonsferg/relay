package compress_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/ext/compress"
)

// zstdEncode is a small helper that compresses data with plain (dict-less)
// zstd, mirroring what the transport under test produces/expects.
func zstdEncode(t *testing.T, data []byte) []byte {
	t.Helper()
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatalf("zstd.NewWriter: %v", err)
	}
	defer enc.Close() //nolint:errcheck
	return enc.EncodeAll(data, nil)
}

func TestWithZstdDictionary_CompressesRequestBody(t *testing.T) {
	t.Parallel()

	const body = "the quick brown fox jumps over the lazy dog"

	var gotEncoding, gotAccept string
	var gotBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEncoding = r.Header.Get("Content-Encoding")
		gotAccept = r.Header.Get("Accept-Encoding")
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("server: read body: %v", err)
		}
		gotBody = raw
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	opt, err := compress.WithZstdDictionary(nil)
	if err != nil {
		t.Fatalf("WithZstdDictionary: %v", err)
	}

	c := relay.New(opt)
	req := c.Post(srv.URL + "/").WithBody([]byte(body))
	if _, err := c.Execute(req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotEncoding != "zstd" {
		t.Errorf("Content-Encoding = %q, want %q", gotEncoding, "zstd")
	}
	if gotAccept == "" || !bytes.Contains([]byte(gotAccept), []byte("zstd")) {
		t.Errorf("Accept-Encoding = %q, want it to contain zstd", gotAccept)
	}

	dec, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatalf("zstd.NewReader: %v", err)
	}
	defer dec.Close()
	got, err := dec.DecodeAll(gotBody, nil)
	if err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	if string(got) != body {
		t.Errorf("request body = %q, want %q", got, body)
	}
}

func TestWithZstdDictionary_DecompressesResponseBody(t *testing.T) {
	t.Parallel()

	const want = "response payload that arrives zstd-compressed from the server"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		compressed := zstdEncode(t, []byte(want))
		w.Header().Set("Content-Encoding", "zstd")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(compressed)
	}))
	defer srv.Close()

	opt, err := compress.WithZstdDictionary(nil)
	if err != nil {
		t.Fatalf("WithZstdDictionary: %v", err)
	}

	c := relay.New(opt)
	resp, err := c.Execute(c.Get(srv.URL + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if resp.Header("Content-Encoding") != "" {
		t.Errorf("Content-Encoding header should be stripped after transparent decompression, got %q", resp.Header("Content-Encoding"))
	}
	if resp.String() != want {
		t.Errorf("response body = %q, want %q", resp.String(), want)
	}
}

func TestWithZstdDictionary_AcceptEncodingAlreadyPresent(t *testing.T) {
	t.Parallel()

	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept-Encoding")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	opt, err := compress.WithZstdDictionary(nil)
	if err != nil {
		t.Fatalf("WithZstdDictionary: %v", err)
	}

	c := relay.New(opt)
	req := c.Get(srv.URL+"/").WithHeader("Accept-Encoding", "gzip")
	if _, err := c.Execute(req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotAccept != "zstd, gzip" {
		t.Errorf("Accept-Encoding = %q, want %q", gotAccept, "zstd, gzip")
	}
}

func TestWithZstdDictionary_AcceptEncodingAlreadyHasZstd(t *testing.T) {
	t.Parallel()

	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept-Encoding")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	opt, err := compress.WithZstdDictionary(nil)
	if err != nil {
		t.Fatalf("WithZstdDictionary: %v", err)
	}

	c := relay.New(opt)
	req := c.Get(srv.URL+"/").WithHeader("Accept-Encoding", "zstd, br")
	if _, err := c.Execute(req); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotAccept != "zstd, br" {
		t.Errorf("Accept-Encoding should not be duplicated, got %q", gotAccept)
	}
}

func TestWithZstdDictionary_NoRequestBody(t *testing.T) {
	t.Parallel()

	var gotEncoding string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEncoding = r.Header.Get("Content-Encoding")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	opt, err := compress.WithZstdDictionary(nil)
	if err != nil {
		t.Fatalf("WithZstdDictionary: %v", err)
	}

	c := relay.New(opt)
	if _, err := c.Execute(c.Get(srv.URL + "/")); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if gotEncoding != "" {
		t.Errorf("Content-Encoding should be unset for a bodyless request, got %q", gotEncoding)
	}
}

func TestWithZstdDictionary_BaseTransportError(t *testing.T) {
	t.Parallel()

	opt, err := compress.WithZstdDictionary(nil)
	if err != nil {
		t.Fatalf("WithZstdDictionary: %v", err)
	}

	// Point at an address nothing is listening on so the base RoundTripper
	// fails and the transport must propagate the error rather than panic.
	c := relay.New(opt, relay.WithDisableRetry())
	_, err = c.Execute(c.Get("http://127.0.0.1:1/"))
	if err == nil {
		t.Fatal("expected error from unreachable base transport, got nil")
	}
}

// buildTestDict produces a real zstd dictionary (with a valid header) from
// a handful of similar samples, mirroring how a pre-trained dictionary would
// be generated for production use.
func buildTestDict(t *testing.T) []byte {
	t.Helper()
	history := bytes.Repeat([]byte("relay dict common phrase alpha beta gamma delta "), 16)
	sample := func(suffix string) []byte {
		b := append([]byte(nil), history...)
		return append(b, suffix...)
	}
	samples := [][]byte{
		sample("status=pending id=1"),
		sample("status=shipped id=2"),
		sample("status=delivered id=3"),
		sample("status=cancelled id=4"),
	}
	dict, err := zstd.BuildDict(zstd.BuildDictOptions{
		ID:       1,
		Contents: samples,
		History:  history,
		Offsets:  [3]int{1, 4, 8},
	})
	if err != nil {
		t.Fatalf("zstd.BuildDict: %v", err)
	}
	return dict
}

func TestWithZstdDictionary_WithDictionary(t *testing.T) {
	t.Parallel()

	dict := buildTestDict(t)

	c, err := compress.NewZstdDictionaryCompressor(dict)
	if err != nil {
		t.Fatalf("NewZstdDictionaryCompressor(dict): %v", err)
	}

	const want = "hello dictionary compressed world"
	compressed, err := c.Compress([]byte(want))
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	got, err := c.Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if c.Encoding() != "zstd" {
		t.Errorf("Encoding() = %q, want zstd", c.Encoding())
	}

	if _, err := compress.WithZstdDictionary(dict); err != nil {
		t.Fatalf("WithZstdDictionary(dict): %v", err)
	}
}

// corruptZstdHandler advertises a zstd-encoded body but writes bytes that
// are not a valid zstd frame, exercising the decompression error path.
type corruptZstdHandler struct{}

func (corruptZstdHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Encoding", "zstd")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("not-a-valid-zstd-frame"))
}

func TestWithZstdDictionary_CorruptResponseBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(corruptZstdHandler{})
	defer srv.Close()

	opt, err := compress.WithZstdDictionary(nil)
	if err != nil {
		t.Fatalf("WithZstdDictionary: %v", err)
	}

	c := relay.New(opt)
	_, err = c.Execute(c.Get(srv.URL + "/"))
	if err == nil {
		t.Fatal("expected decompression error for a corrupt zstd body, got nil")
	}
}
