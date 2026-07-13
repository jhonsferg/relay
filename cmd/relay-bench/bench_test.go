package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBuildStats_Empty(t *testing.T) {
	t.Parallel()

	s := buildStats(nil, 0)
	if s.Total != 0 {
		t.Errorf("Total = %d, want 0", s.Total)
	}
	if s.Successes != 0 || s.Failures != 0 || s.Errors != 0 {
		t.Errorf("expected all-zero counters for empty results, got %+v", s)
	}
	if s.StatusCodes == nil {
		t.Error("StatusCodes map should be initialized, got nil")
	}
}

func TestBuildStats_MixedResults(t *testing.T) {
	t.Parallel()

	results := []result{
		{latency: 10 * time.Millisecond, statusCode: 200},
		{latency: 20 * time.Millisecond, statusCode: 200},
		{latency: 30 * time.Millisecond, statusCode: 404},
		{err: errors.New("dial tcp: connection refused")},
	}

	s := buildStats(results, time.Second)

	if s.Total != 4 {
		t.Errorf("Total = %d, want 4", s.Total)
	}
	if s.Successes != 2 {
		t.Errorf("Successes = %d, want 2", s.Successes)
	}
	if s.Failures != 1 {
		t.Errorf("Failures = %d, want 1", s.Failures)
	}
	if s.Errors != 1 {
		t.Errorf("Errors = %d, want 1", s.Errors)
	}
	if s.StatusCodes[200] != 2 {
		t.Errorf("StatusCodes[200] = %d, want 2", s.StatusCodes[200])
	}
	if s.StatusCodes[404] != 1 {
		t.Errorf("StatusCodes[404] = %d, want 1", s.StatusCodes[404])
	}
	if time.Duration(s.LatencyMin) != 10*time.Millisecond {
		t.Errorf("LatencyMin = %v, want 10ms", time.Duration(s.LatencyMin))
	}
	if time.Duration(s.LatencyMax) != 30*time.Millisecond {
		t.Errorf("LatencyMax = %v, want 30ms", time.Duration(s.LatencyMax))
	}
	if s.RPS != 4 {
		t.Errorf("RPS = %v, want 4 (4 total / 1s elapsed)", s.RPS)
	}
}

func TestBuildStats_ZeroElapsed(t *testing.T) {
	t.Parallel()

	results := []result{{latency: time.Millisecond, statusCode: 200}}
	s := buildStats(results, 0)
	if s.RPS != 0 {
		t.Errorf("RPS = %v, want 0 when elapsed is 0", s.RPS)
	}
}

func TestPercentile(t *testing.T) {
	t.Parallel()

	sorted := []time.Duration{
		1 * time.Millisecond,
		2 * time.Millisecond,
		3 * time.Millisecond,
		4 * time.Millisecond,
		5 * time.Millisecond,
	}

	tests := []struct {
		p    float64
		want time.Duration
	}{
		{p: 50, want: 3 * time.Millisecond},
		{p: 95, want: 5 * time.Millisecond},
		{p: 99, want: 5 * time.Millisecond},
		{p: 0, want: 1 * time.Millisecond},
		{p: 100, want: 5 * time.Millisecond},
	}
	for _, tt := range tests {
		got := percentile(sorted, tt.p)
		if got != tt.want {
			t.Errorf("percentile(_, %v) = %v, want %v", tt.p, got, tt.want)
		}
	}
}

func TestPercentile_Empty(t *testing.T) {
	t.Parallel()

	if got := percentile(nil, 50); got != 0 {
		t.Errorf("percentile(nil, 50) = %v, want 0", got)
	}
}

func TestSortedIntKeys(t *testing.T) {
	t.Parallel()

	m := map[int]int{500: 1, 200: 5, 404: 2}
	got := sortedIntKeys(m)
	want := []int{200, 404, 500}
	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestSortedIntKeys_Empty(t *testing.T) {
	t.Parallel()

	got := sortedIntKeys(map[int]int{})
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestDuration_MarshalJSON(t *testing.T) {
	t.Parallel()

	d := duration(1500 * time.Millisecond)
	b, err := d.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	want := `"1.5s"`
	if string(b) != want {
		t.Errorf("MarshalJSON() = %s, want %s", b, want)
	}
}

func TestPrintStats(t *testing.T) {
	// Mutates the package-level os.Stdout, so this test must not run in
	// parallel with anything else that writes to stdout.
	s := &Stats{
		URL:         "https://example.com",
		Method:      "GET",
		Concurrency: 10,
		Total:       100,
		Successes:   90,
		Failures:    8,
		Errors:      2,
		RPS:         123.45,
		StatusCodes: map[int]int{200: 90, 500: 8},
		CBTransitions: []string{
			"closed->open",
		},
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	printStats(s)

	w.Close() //nolint:errcheck
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}

	got := string(out)
	if !strings.Contains(got, "relay-bench results") {
		t.Errorf("output missing header, got:\n%s", got)
	}
	if !strings.Contains(got, "https://example.com") {
		t.Errorf("output missing URL, got:\n%s", got)
	}
	if !strings.Contains(got, "HTTP 200") || !strings.Contains(got, "HTTP 500") {
		t.Errorf("output missing status code lines, got:\n%s", got)
	}
	if !strings.Contains(got, "closed->open") {
		t.Errorf("output missing circuit breaker transition, got:\n%s", got)
	}
}
