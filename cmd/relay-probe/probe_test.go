package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jhonsferg/relay"
)

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		max  int
		want string
	}{
		{in: "short", max: 40, want: "short"},
		{in: "exactly-ten", max: 11, want: "exactly-ten"},
		{in: "this is a rather long url that needs cutting", max: 10, want: "this is a…"},
	}
	for _, tt := range tests {
		if got := truncate(tt.in, tt.max); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
		}
	}
}

func TestCheck_Healthy(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New(relay.WithDisableRetry())
	defer func() { _ = client.Shutdown(context.Background()) }()
	p := &probe{url: srv.URL, client: client}

	r := check(context.Background(), p, checkConfig{expectedStatus: 200})
	if !r.Healthy {
		t.Errorf("expected healthy result, got %+v", r)
	}
	if r.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", r.StatusCode)
	}
}

func TestCheck_UnexpectedStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := relay.New(relay.WithDisableRetry())
	defer func() { _ = client.Shutdown(context.Background()) }()
	p := &probe{url: srv.URL, client: client}

	r := check(context.Background(), p, checkConfig{expectedStatus: 200, verbose: true})
	if r.Healthy {
		t.Error("expected unhealthy result for a 500 response")
	}
	if r.Reason == "" {
		t.Error("expected a reason to be set")
	}
}

func TestCheck_LatencyExceeded(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := relay.New(relay.WithDisableRetry())
	defer func() { _ = client.Shutdown(context.Background()) }()
	p := &probe{url: srv.URL, client: client}

	r := check(context.Background(), p, checkConfig{expectedStatus: 200, maxLatency: time.Millisecond})
	if r.Healthy {
		t.Error("expected unhealthy result when latency exceeds threshold")
	}
	if !strings.Contains(r.Reason, "exceeds threshold") {
		t.Errorf("Reason = %q, want it to mention exceeding threshold", r.Reason)
	}
}

func TestCheck_RequestError(t *testing.T) {
	t.Parallel()

	client := relay.New(relay.WithDisableRetry(), relay.WithTimeout(200*time.Millisecond))
	defer func() { _ = client.Shutdown(context.Background()) }()
	p := &probe{url: "http://127.0.0.1:1/", client: client}

	r := check(context.Background(), p, checkConfig{expectedStatus: 200, verbose: true})
	if r.Healthy {
		t.Error("expected unhealthy result for an unreachable endpoint")
	}
	if r.Error == "" {
		t.Error("expected Error to be populated")
	}
	if r.Reason != "request failed" {
		t.Errorf("Reason = %q, want %q", r.Reason, "request failed")
	}
}

func TestCheck_CircuitBreakerOpen(t *testing.T) {
	t.Parallel()

	client := relay.New(relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
		MaxFailures:      1,
		ResetTimeout:     time.Hour,
		HalfOpenRequests: 1,
		SuccessThreshold: 1,
	}))
	defer func() { _ = client.Shutdown(context.Background()) }()
	p := &probe{url: "http://127.0.0.1:1/", client: client}

	// Trip the breaker with one failing request.
	_ = check(context.Background(), p, checkConfig{expectedStatus: 200})

	r := check(context.Background(), p, checkConfig{expectedStatus: 200, verbose: true})
	if !r.CBOpen {
		t.Errorf("expected circuit breaker to report open, got %+v", r)
	}
	if r.Reason != "circuit breaker open" {
		t.Errorf("Reason = %q, want %q", r.Reason, "circuit breaker open")
	}
}

func TestRunChecks_Concurrent(t *testing.T) {
	t.Parallel()

	srvOK := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srvOK.Close()
	srvFail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srvFail.Close()

	c1 := relay.New(relay.WithDisableRetry())
	c2 := relay.New(relay.WithDisableRetry())
	defer func() { _ = c1.Shutdown(context.Background()) }()
	defer func() { _ = c2.Shutdown(context.Background()) }()

	probes := []*probe{
		{url: srvOK.URL, client: c1},
		{url: srvFail.URL, client: c2},
	}

	results := runChecks(context.Background(), probes, checkConfig{expectedStatus: 200})
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if !results[0].Healthy {
		t.Errorf("results[0] should be healthy, got %+v", results[0])
	}
	if results[1].Healthy {
		t.Errorf("results[1] should be unhealthy, got %+v", results[1])
	}
}

func TestPrintReport_HumanReadable(t *testing.T) {
	// Mutates os.Stdout; must not run in parallel with other stdout users.
	results := []CheckResult{
		{URL: "https://ok.example.com", Healthy: true, StatusCode: 200, LatencyHuman: "5ms"},
		{URL: "https://bad.example.com", Healthy: false, Reason: "boom"},
		{URL: "https://cb.example.com", CBOpen: true},
	}

	out := captureStdout(t, func() {
		code := printReport(results, false)
		if code != 2 {
			t.Errorf("exit code = %d, want 2 (circuit breaker open takes precedence)", code)
		}
	})

	if !strings.Contains(out, "ok.example.com") || !strings.Contains(out, "bad.example.com") {
		t.Errorf("output missing endpoint URLs, got:\n%s", out)
	}
	if !strings.Contains(out, "UNHEALTHY") {
		t.Errorf("output missing UNHEALTHY status, got:\n%s", out)
	}
}

func TestPrintReport_AllHealthy(t *testing.T) {
	results := []CheckResult{
		{URL: "https://ok.example.com", Healthy: true, StatusCode: 200},
	}
	out := captureStdout(t, func() {
		code := printReport(results, false)
		if code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
	})
	if !strings.Contains(out, "HEALTHY") {
		t.Errorf("output missing HEALTHY status, got:\n%s", out)
	}
}

func TestPrintReport_JSON(t *testing.T) {
	results := []CheckResult{
		{URL: "https://ok.example.com", Healthy: true, StatusCode: 200},
	}
	out := captureStdout(t, func() {
		code := printReport(results, true)
		if code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
	})

	var report struct {
		Healthy bool          `json:"healthy"`
		Results []CheckResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if !report.Healthy {
		t.Error("expected healthy=true in JSON report")
	}
	if len(report.Results) != 1 {
		t.Errorf("len(Results) = %d, want 1", len(report.Results))
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	w.Close() //nolint:errcheck
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	return string(out)
}
