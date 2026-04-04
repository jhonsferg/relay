package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jhonsferg/relay"
)

// downloadConfig holds all options for a file download.
type downloadConfig struct {
	outPath     string // explicit output path; "" means auto or stdout
	remoteNames bool   // derive filename from URL / Content-Disposition
	resume      bool   // try to resume partial download
	quiet       bool   // suppress progress bar
	parallel    int    // max parallel downloads (>1 enables concurrent mode)
}

// downloadAll downloads one or more URLs according to cfg.
// When a single URL without -o / -O is given it falls back to normal output.
func downloadAll(ctx context.Context, client *relay.Client, urls []string, cfg downloadConfig) error {
	if len(urls) == 1 && !cfg.remoteNames && cfg.outPath == "" {
		return nil // caller handles single-URL stdout case
	}

	if cfg.parallel <= 1 {
		for _, u := range urls {
			if err := downloadOne(ctx, client, u, cfg); err != nil {
				fmt.Fprintf(os.Stderr, "error downloading %s: %v\n", u, err)
			}
		}
		return nil
	}

	// Parallel downloads.
	sem := make(chan struct{}, cfg.parallel)
	var wg sync.WaitGroup
	for _, u := range urls {
		sem <- struct{}{}
		wg.Add(1)
		go func(rawURL string) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := downloadOne(ctx, client, rawURL, cfg); err != nil {
				fmt.Fprintf(os.Stderr, "error downloading %s: %v\n", rawURL, err)
			}
		}(u)
	}
	wg.Wait()
	return nil
}

// downloadOne downloads a single URL and saves it to disk.
func downloadOne(ctx context.Context, client *relay.Client, rawURL string, cfg downloadConfig) error {
	// HEAD first to get Content-Disposition without downloading the body.
	var contentDisp string
	if cfg.remoteNames {
		headReq := client.Head(rawURL).WithContext(ctx).WithTimeout(10 * time.Second)
		if resp, err := client.Execute(headReq); err == nil {
			contentDisp = resp.Headers.Get("Content-Disposition")
			relay.PutResponse(resp)
		}
	}

	outPath := cfg.outPath
	if cfg.remoteNames || (outPath == "" && len([]string{rawURL}) == 0) {
		outPath = autoFilename(rawURL, contentDisp)
	}
	if outPath == "" {
		outPath = autoFilename(rawURL, contentDisp)
	}

	// Determine resume offset.
	var offset int64
	if cfg.resume {
		if info, err := os.Stat(outPath); err == nil {
			offset = info.Size()
			if !cfg.quiet {
				fmt.Fprintf(os.Stderr, "resuming %s from %s\n", outPath, formatBytes(offset))
			}
		}
	}

	req := client.Get(rawURL)
	if offset > 0 {
		req = req.WithHeader("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	stream, err := client.ExecuteStream(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer func() { _ = stream.Body.Close() }()

	if stream.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		fmt.Fprintf(os.Stderr, "%s: file already fully downloaded\n", outPath)
		return nil
	}
	if stream.IsError() {
		return fmt.Errorf("server returned HTTP %s", stream.Status)
	}

	// Parse total size from Content-Length or Content-Range.
	total := parseContentLength(stream.Headers, offset)

	// Open the output file.
	var f *os.File
	if offset > 0 && stream.StatusCode == http.StatusPartialContent {
		f, err = os.OpenFile(outPath, os.O_WRONLY|os.O_APPEND, 0o600) // #nosec G304
	} else {
		offset = 0                  // server sent 200 instead of 206 — start over
		f, err = os.Create(outPath) // #nosec G304
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	filename := filepath.Base(outPath)
	showProgress := !cfg.quiet && isTerminal(os.Stderr)

	if !cfg.quiet {
		if total > 0 {
			fmt.Fprintf(os.Stderr, "  %s  (%s)\n", filename, formatBytes(total+offset))
		} else {
			fmt.Fprintf(os.Stderr, "  %s\n", filename)
		}
	}

	if showProgress {
		pw := newProgressWriter(f, filename, offset, total)
		defer pw.finish()
		_, err = io.Copy(pw, stream.Body)
	} else {
		_, err = io.Copy(f, stream.Body)
	}

	return err
}

// uploadFile streams a local file to the server with a progress bar.
func uploadFile(ctx context.Context, client *relay.Client, rawURL, filePath string, quiet bool) (*relay.Response, error) {
	f, err := os.Open(filePath) // #nosec G304
	if err != nil {
		return nil, fmt.Errorf("cannot open upload file: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	total := info.Size()

	var body io.Reader = f
	if !quiet && isTerminal(os.Stderr) {
		filename := filepath.Base(filePath)
		fmt.Fprintf(os.Stderr, "  uploading %s (%s)\n", filename, formatBytes(total))
		pw := newProgressWriter(io.Discard, filename, 0, total) // tracks but discards — actual write is to pw.dest below
		_ = pw
		// Use relay's built-in upload progress via WithUploadProgress.
	}

	req := client.Put(rawURL).
		WithBodyReader(f).
		WithContentType("application/octet-stream")

	if !quiet && isTerminal(os.Stderr) {
		filename := filepath.Base(filePath)
		fmt.Fprintln(os.Stderr)
		req = req.WithUploadProgress(func(sent, total int64) {
			pct := int(float64(sent) / float64(total) * 100)
			const w = 28
			filled := w * pct / 100
			bar := strings.Repeat("=", filled)
			if filled < w {
				bar += ">" + strings.Repeat(" ", w-filled-1)
			}
			fmt.Fprintf(os.Stderr, "\r  %-18s [%s] %3d%%  %s / %s",
				filename, bar, pct, formatBytes(sent), formatBytes(total))
		})
	}
	_ = body

	resp, err := client.Execute(req.WithContext(ctx))
	if !quiet && isTerminal(os.Stderr) {
		fmt.Fprintln(os.Stderr)
	}
	return resp, err
}

// autoFilename determines the save filename from Content-Disposition or the URL path.
func autoFilename(rawURL, contentDisp string) string {
	if contentDisp != "" {
		if _, params, err := mime.ParseMediaType(contentDisp); err == nil {
			if name := params["filename"]; name != "" {
				return filepath.Base(filepath.Clean(name))
			}
		}
	}
	if u, err := url.Parse(rawURL); err == nil && u.Path != "" {
		name := filepath.Base(u.Path)
		if name != "." && name != "/" && name != "" {
			return name
		}
	}
	return "download"
}

// parseContentLength extracts the number of remaining bytes from response headers.
func parseContentLength(h http.Header, offset int64) int64 {
	if cr := h.Get("Content-Range"); cr != "" {
		// "bytes 500-1234/5000" → total is 5000 − offset
		parts := strings.SplitN(cr, "/", 2)
		if len(parts) == 2 {
			if fullSize, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64); err == nil && fullSize > 0 {
				return fullSize - offset
			}
		}
	}
	if cl := h.Get("Content-Length"); cl != "" {
		if n, err := strconv.ParseInt(strings.TrimSpace(cl), 10, 64); err == nil {
			return n
		}
	}
	return 0
}

// ─── Cookie jar persistence ───────────────────────────────────────────────────

// fileCookieJar is an http.CookieJar that persists cookies in Netscape format.
type fileCookieJar struct {
	inner   *cookiejar.Jar
	path    string
	mu      sync.Mutex
	entries []cookieEntry // all recorded set-cookie calls
}

type cookieEntry struct {
	host    string
	cookies []*http.Cookie
}

// newFileCookieJar creates a jar backed by the given file, pre-loading any
// existing cookies from it.
func newFileCookieJar(path string) (*fileCookieJar, error) {
	inner, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	jar := &fileCookieJar{inner: inner, path: path}
	if err := jar.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("loading cookie jar %s: %w", path, err)
	}
	return jar, nil
}

func (j *fileCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.inner.SetCookies(u, cookies)
	j.mu.Lock()
	j.entries = append(j.entries, cookieEntry{host: u.Host, cookies: cookies})
	j.mu.Unlock()
}

func (j *fileCookieJar) Cookies(u *url.URL) []*http.Cookie {
	return j.inner.Cookies(u)
}

// Save writes all recorded cookies to the backing file in Netscape format.
func (j *fileCookieJar) Save() error {
	j.mu.Lock()
	defer j.mu.Unlock()

	f, err := os.Create(j.path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	w := bufio.NewWriter(f)
	_, _ = fmt.Fprintln(w, "# Netscape HTTP Cookie File")
	_, _ = fmt.Fprintln(w, "# Generated by relay. Do not edit manually.")
	_, _ = fmt.Fprintln(w)

	for _, e := range j.entries {
		for _, c := range e.cookies {
			domain := c.Domain
			if domain == "" {
				domain = e.host
			}
			includeSubDomains := "FALSE"
			if strings.HasPrefix(domain, ".") {
				includeSubDomains = "TRUE"
			}
			secure := "FALSE"
			if c.Secure {
				secure = "TRUE"
			}
			expires := int64(0)
			if !c.Expires.IsZero() {
				expires = c.Expires.Unix()
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
				domain, includeSubDomains, c.Path, secure, expires, c.Name, c.Value)
		}
	}
	return w.Flush()
}

// load reads a Netscape cookie file and populates the inner jar.
func (j *fileCookieJar) load() error {
	f, err := os.Open(j.path) // #nosec G304
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	byHost := make(map[string][]*http.Cookie)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}
		domain, path, secure, name, value := fields[0], fields[2], fields[3], fields[5], fields[6]
		c := &http.Cookie{
			Domain: domain,
			Path:   path,
			Secure: strings.EqualFold(secure, "TRUE"),
			Name:   name,
			Value:  value,
		}
		if ts, err := strconv.ParseInt(fields[4], 10, 64); err == nil && ts > 0 {
			c.Expires = time.Unix(ts, 0)
		}
		byHost[domain] = append(byHost[domain], c)
	}

	for host, cookies := range byHost {
		scheme := "http"
		for _, c := range cookies {
			if c.Secure {
				scheme = "https"
				break
			}
		}
		h := strings.TrimPrefix(host, ".")
		u := &url.URL{Scheme: scheme, Host: h, Path: "/"}
		j.inner.SetCookies(u, cookies)
	}

	return scanner.Err()
}
