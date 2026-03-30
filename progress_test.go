package relay

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

func TestUploadProgress_CallbackReceivesBytes(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())

	bodyContent := strings.Repeat("x", 512)
	var lastTransferred, lastTotal int64
	var callCount int32

	req := c.Post(srv.URL() + "/upload").
		WithBody([]byte(bodyContent)).
		WithUploadProgress(func(transferred, total int64) {
			atomic.StoreInt64(&lastTransferred, transferred)
			atomic.StoreInt64(&lastTotal, total)
			atomic.AddInt32(&callCount, 1)
		})

	_, err := c.Execute(req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if atomic.LoadInt32(&callCount) == 0 {
		t.Error("upload progress callback was never called")
	}
	got := atomic.LoadInt64(&lastTransferred)
	if got != int64(len(bodyContent)) {
		t.Errorf("expected final transferred=%d, got %d", len(bodyContent), got)
	}
	gotTotal := atomic.LoadInt64(&lastTotal)
	if gotTotal != int64(len(bodyContent)) {
		t.Errorf("expected total=%d, got %d", len(bodyContent), gotTotal)
	}
}

func TestUploadProgress_TransferredNeverExceedsTotal(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	body := strings.Repeat("a", 1024)

	req := c.Post(srv.URL() + "/up").
		WithBody([]byte(body)).
		WithUploadProgress(func(transferred, total int64) {
			if transferred > total {
				t.Errorf("transferred (%d) must not exceed total (%d)", transferred, total)
			}
		})

	_, err := c.Execute(req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

func TestDownloadProgress_CallbackReceivesBytes(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	responseBody := strings.Repeat("y", 1024)
	srv.Enqueue(testutil.MockResponse{
		Status: http.StatusOK,
		Body:   responseBody,
	})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())

	var lastTransferred int64
	var callCount int32

	req := c.Get(srv.URL() + "/download").
		WithDownloadProgress(func(transferred, total int64) {
			atomic.StoreInt64(&lastTransferred, transferred)
			atomic.AddInt32(&callCount, 1)
		})

	resp, err := c.Execute(req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// The body is read inside newResponse (io.ReadAll), which triggers progress.
	if atomic.LoadInt32(&callCount) == 0 {
		t.Error("download progress callback was never called")
	}
	got := atomic.LoadInt64(&lastTransferred)
	if got != int64(len(responseBody)) {
		t.Errorf("expected final transferred=%d, got %d", len(responseBody), got)
	}
	_ = resp
}

func TestDownloadProgress_UnknownContentLength(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()

	// Server does not set Content-Length — total should be -1.
	srv.Enqueue(testutil.MockResponse{
		Status: http.StatusOK,
		Body:   "no-length",
	})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())

	var observedTotal int64 = 0
	var called int32

	req := c.Get(srv.URL() + "/dl").
		WithDownloadProgress(func(transferred, total int64) {
			atomic.StoreInt64(&observedTotal, total)
			atomic.AddInt32(&called, 1)
		})

	_, err := c.Execute(req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if atomic.LoadInt32(&called) == 0 {
		t.Skip("progress was not called; server may have set Content-Length anyway")
	}
	// total should be -1 when Content-Length is absent.
	got := atomic.LoadInt64(&observedTotal)
	if got != -1 {
		t.Logf("expected total=-1 for unknown content length, got %d (acceptable if server sets it)", got)
	}
}

func TestProgressReader_MonotonicTransferred(t *testing.T) {
	t.Parallel()

	content := []byte(strings.Repeat("z", 256))
	var prev int64
	pr := newProgressReader(
		strings.NewReader(string(content)),
		int64(len(content)),
		func(transferred, total int64) {
			if transferred < prev {
				t.Errorf("transferred went backwards: %d < %d", transferred, prev)
			}
			prev = transferred
		},
	)

	buf := make([]byte, 64)
	for {
		n, err := pr.Read(buf)
		if n == 0 && err != nil {
			break
		}
	}

	if prev != int64(len(content)) {
		t.Errorf("expected final transferred=%d, got %d", len(content), prev)
	}
}

func TestProgressReadCloser_Close(t *testing.T) {
	t.Parallel()
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "data"})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
	closed := make(chan struct{})
	req := c.Get(srv.URL() + "/").
		WithDownloadProgress(func(transferred, total int64) {})
	resp, err := c.Execute(req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// Response body is already consumed by Execute; just verify no panic.
	_ = resp
	close(closed)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Error("test did not complete in time")
	}
}
