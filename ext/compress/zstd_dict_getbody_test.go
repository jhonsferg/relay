package compress_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/ext/compress"
)

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// TestWithZstdDictionary_GetBodyMatchesSentBody guards against a bug where
// r.GetBody (inherited via req.Clone before compression) still returned the
// original, uncompressed bytes after RoundTrip replaced r.Body/ContentLength
// /Content-Encoding with the compressed version - a redirect requiring body
// resend would replay uncompressed bytes mislabeled as zstd with a mismatched
// Content-Length.
func TestWithZstdDictionary_GetBodyMatchesSentBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	opt, err := compress.WithZstdDictionary(nil)
	if err != nil {
		t.Fatalf("WithZstdDictionary: %v", err)
	}

	var captured *http.Request
	// Added after opt so it becomes the innermost middleware, receiving the
	// request exactly as zstd's transport hands it to the real transport.
	capture := relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(req *http.Request) (*http.Response, error) {
			captured = req
			return next.RoundTrip(req)
		})
	})

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		opt,
		capture,
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	original := strings.Repeat("compress me please ", 200) // large enough to actually compress
	_, err = client.Execute(client.Post("/").WithBody([]byte(original)))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if captured == nil {
		t.Fatal("capture middleware never ran")
	}
	if captured.GetBody == nil {
		t.Fatal("GetBody is nil on the compressed request")
	}

	rc, err := captured.GetBody()
	if err != nil {
		t.Fatalf("GetBody(): %v", err)
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read GetBody() result: %v", err)
	}

	if int64(len(got)) != captured.ContentLength {
		t.Errorf("GetBody() returned %d bytes, want %d (matching Content-Length %q)",
			len(got), captured.ContentLength, captured.Header.Get("Content-Encoding"))
	}
	if bytes.Equal(got, []byte(original)) {
		t.Error("GetBody() returned the original uncompressed bytes, not the compressed body actually sent")
	}
}
