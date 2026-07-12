package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhonsferg/relay"
)

func TestAutoFilename(t *testing.T) {
	cases := []struct {
		name        string
		rawURL      string
		contentDisp string
		want        string
	}{
		{"content-disposition wins", "https://example.com/foo", `attachment; filename="report.pdf"`, "report.pdf"},
		{"content-disposition path traversal is cleaned", "https://example.com/foo", `attachment; filename="../../etc/passwd"`, "passwd"},
		{"falls back to URL path", "https://example.com/dir/archive.zip", "", "archive.zip"},
		{"falls back to URL path with query", "https://example.com/dir/archive.zip?x=1", "", "archive.zip"},
		{"empty path falls back to default", "https://example.com", "", "download"},
		{"invalid url falls back to default", "://bad-url", "", "download"},
		{"malformed content-disposition falls back to url", "https://example.com/x.txt", "not-a-valid-header", "x.txt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := autoFilename(c.rawURL, c.contentDisp)
			if got != c.want {
				t.Errorf("autoFilename(%q, %q) = %q, want %q", c.rawURL, c.contentDisp, got, c.want)
			}
		})
	}
}

func TestParseContentLength(t *testing.T) {
	cases := []struct {
		name   string
		header http.Header
		offset int64
		want   int64
	}{
		{
			name:   "content-range wins",
			header: http.Header{"Content-Range": []string{"bytes 500-1234/5000"}},
			offset: 500,
			want:   4500,
		},
		{
			name:   "content-length fallback",
			header: http.Header{"Content-Length": []string{"1024"}},
			offset: 0,
			want:   1024,
		},
		{
			name:   "malformed content-range falls back to content-length",
			header: http.Header{"Content-Range": []string{"garbage"}, "Content-Length": []string{"42"}},
			offset: 0,
			want:   42,
		},
		{
			name:   "no headers",
			header: http.Header{},
			offset: 0,
			want:   0,
		},
		{
			name:   "invalid content-length",
			header: http.Header{"Content-Length": []string{"not-a-number"}},
			offset: 0,
			want:   0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseContentLength(c.header, c.offset)
			if got != c.want {
				t.Errorf("parseContentLength() = %d, want %d", got, c.want)
			}
		})
	}
}

func TestFileCookieJarSetAndCookies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cookies.txt")

	jar, err := newFileCookieJar(path)
	if err != nil {
		t.Fatalf("newFileCookieJar: %v", err)
	}

	u, _ := url.Parse("https://example.com/")
	cookies := []*http.Cookie{{Name: "session", Value: "abc123"}}
	jar.SetCookies(u, cookies)

	got := jar.Cookies(u)
	if len(got) != 1 || got[0].Value != "abc123" {
		t.Fatalf("Cookies() = %+v, want session=abc123", got)
	}
}

func TestFileCookieJarSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cookies.txt")

	jar, err := newFileCookieJar(path)
	if err != nil {
		t.Fatalf("newFileCookieJar: %v", err)
	}

	u, _ := url.Parse("https://example.com/")
	jar.SetCookies(u, []*http.Cookie{
		{Name: "session", Value: "abc123", Secure: true},
		{Name: "plain", Value: "xyz", Domain: ".example.com"},
	})

	if err := jar.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading saved cookie file: %v", err)
	}
	if !strings.Contains(string(data), "session") || !strings.Contains(string(data), "abc123") {
		t.Errorf("saved file missing expected cookie data: %s", data)
	}

	// Reload into a fresh jar and confirm cookies come back.
	jar2, err := newFileCookieJar(path)
	if err != nil {
		t.Fatalf("newFileCookieJar (reload): %v", err)
	}
	reloaded := jar2.Cookies(u)
	if len(reloaded) == 0 {
		t.Error("expected cookies to be reloaded from disk")
	}
}

func TestNewFileCookieJarNonExistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "does-not-exist.txt")

	// Loading a jar backed by a nonexistent file should succeed (empty jar).
	jar, err := newFileCookieJar(path)
	if err != nil {
		t.Fatalf("newFileCookieJar with missing file: %v", err)
	}
	if jar == nil {
		t.Fatal("expected non-nil jar")
	}
}

func TestDownloadOne(t *testing.T) {
	body := []byte("hello world, this is the downloaded content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.bin")

	client := relay.New()
	defer func() { _ = client.Shutdown(context.Background()) }()

	err := downloadOne(context.Background(), client, srv.URL, downloadConfig{
		outPath: outPath,
		quiet:   true,
	})
	if err != nil {
		t.Fatalf("downloadOne: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading downloaded file: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("downloaded content = %q, want %q", got, body)
	}
}

func TestDownloadOneServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.bin")

	client := relay.New()
	defer func() { _ = client.Shutdown(context.Background()) }()

	err := downloadOne(context.Background(), client, srv.URL, downloadConfig{
		outPath: outPath,
		quiet:   true,
	})
	if err == nil {
		t.Fatal("expected error for HTTP 500 response")
	}
}

func TestDownloadAllSingleURLNoOutput(t *testing.T) {
	client := relay.New()
	defer func() { _ = client.Shutdown(context.Background()) }()

	err := downloadAll(context.Background(), client, []string{"https://example.com/x"}, downloadConfig{})
	if err != nil {
		t.Fatalf("downloadAll should no-op for single URL without -o/-O: %v", err)
	}
}

// chdirTemp switches the process working directory to a fresh temp dir for
// the duration of the test and restores it afterward. downloadOne with
// remoteNames=true always derives the output path from the URL and writes
// relative to the current working directory, so tests exercising that path
// must not pollute the repo.
func chdirTemp(t *testing.T) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestDownloadAllSequential(t *testing.T) {
	chdirTemp(t)

	body := []byte("content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	client := relay.New()
	defer func() { _ = client.Shutdown(context.Background()) }()

	cfg := downloadConfig{remoteNames: true, quiet: true}
	err := downloadAll(context.Background(), client, []string{srv.URL + "/a", srv.URL + "/b"}, cfg)
	if err != nil {
		t.Fatalf("downloadAll: %v", err)
	}
}

func TestDownloadAllParallel(t *testing.T) {
	chdirTemp(t)

	body := []byte("content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	client := relay.New()
	defer func() { _ = client.Shutdown(context.Background()) }()

	cfg := downloadConfig{remoteNames: true, quiet: true, parallel: 4}
	err := downloadAll(context.Background(), client, []string{srv.URL + "/a", srv.URL + "/b", srv.URL + "/c"}, cfg)
	if err != nil {
		t.Fatalf("downloadAll parallel: %v", err)
	}
}

func TestUploadFile(t *testing.T) {
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		received = buf
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	srcPath := filepath.Join(dir, "upload.bin")
	if err := os.WriteFile(srcPath, []byte("upload me"), 0o600); err != nil {
		t.Fatal(err)
	}

	client := relay.New()
	defer func() { _ = client.Shutdown(context.Background()) }()

	resp, err := uploadFile(context.Background(), client, srv.URL, srcPath, true)
	if err != nil {
		t.Fatalf("uploadFile: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	_ = received
}

func TestUploadFileMissingSource(t *testing.T) {
	client := relay.New()
	defer func() { _ = client.Shutdown(context.Background()) }()

	_, err := uploadFile(context.Background(), client, "https://example.com/", "/no/such/file", true)
	if err == nil {
		t.Fatal("expected error opening nonexistent upload file")
	}
}
