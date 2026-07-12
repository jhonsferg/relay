package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{-5, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
	}
	for _, c := range cases {
		if got := formatBytes(c.in); got != c.want {
			t.Errorf("formatBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{5 * time.Second, "0:05"},
		{83 * time.Second, "1:23"},
		{3661 * time.Second, "1:01:01"},
	}
	for _, c := range cases {
		if got := formatDuration(c.in); got != c.want {
			t.Errorf("formatDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsTerminal(t *testing.T) {
	// A regular file is never a character device.
	f, err := os.CreateTemp(t.TempDir(), "not-a-terminal")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	if isTerminal(f) {
		t.Error("isTerminal(regular file) = true, want false")
	}
}

func TestNewProgressWriter(t *testing.T) {
	var buf bytes.Buffer
	pw := newProgressWriter(&buf, "file.bin", 10, 100)
	if pw.dest != &buf {
		t.Error("dest not set")
	}
	if pw.filename != "file.bin" {
		t.Errorf("filename = %q, want %q", pw.filename, "file.bin")
	}
	if pw.offset != 10 || pw.total != 100 {
		t.Errorf("offset/total = %d/%d, want 10/100", pw.offset, pw.total)
	}
	if pw.startTime.IsZero() {
		t.Error("startTime not set")
	}
}

func TestProgressWriterWrite(t *testing.T) {
	var buf bytes.Buffer
	pw := newProgressWriter(&buf, "file.bin", 0, 100)

	n, err := pw.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != 5 {
		t.Errorf("Write returned n=%d, want 5", n)
	}
	if pw.written != 5 {
		t.Errorf("written = %d, want 5", pw.written)
	}
	if buf.String() != "hello" {
		t.Errorf("dest content = %q, want %q", buf.String(), "hello")
	}
}

func TestProgressWriterFinish(t *testing.T) {
	var buf bytes.Buffer
	pw := newProgressWriter(&buf, "file.bin", 0, 100)

	if pw.done {
		t.Fatal("done should start false")
	}
	pw.finish()
	if !pw.done {
		t.Error("finish() did not set done")
	}

	// Calling finish again should be a no-op (no panic, no double render).
	pw.finish()
}

func TestProgressWriterRenderKnownTotal(t *testing.T) {
	var buf bytes.Buffer
	pw := newProgressWriter(&buf, "a-very-long-filename.bin", 0, 100)
	pw.written = 50
	pw.render()
	// render writes to os.Stderr directly; just make sure it doesn't panic
	// and lastLineLen was updated.
	if pw.lastLineLen == 0 {
		t.Error("lastLineLen was not updated by render")
	}
}

func TestProgressWriterRenderUnknownTotal(t *testing.T) {
	var buf bytes.Buffer
	pw := newProgressWriter(&buf, "file.bin", 0, 0)
	pw.written = 50
	pw.render()
	if pw.lastLineLen == 0 {
		t.Error("lastLineLen was not updated by render")
	}
}

func TestProgressWriterRenderCompletion(t *testing.T) {
	var buf bytes.Buffer
	pw := newProgressWriter(&buf, "file.bin", 0, 100)
	pw.written = 100
	pw.render() // exercises the pct>=100 "Done" ETA branch
}

func TestFormatBytesUnits(t *testing.T) {
	// Sanity-check that increasing magnitude walks through unit suffixes.
	got := formatBytes(1024 * 1024 * 1024 * 1024)
	if !strings.HasSuffix(got, "TB") {
		t.Errorf("formatBytes(1TB) = %q, want suffix TB", got)
	}
}
