// Package main implements a feature-rich HTTP client powered by the relay
// library, exposing retry, circuit-breaker, rate-limit, signing, streaming,
// download/upload and timing capabilities from the command line.
package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/jhonsferg/relay"
)

const version = "0.1.1"

// multiFlag accumulates repeated flag values (e.g. -H "K: V" -H "K2: V2").
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ", ") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	os.Exit(run(os.Args[1:]))
}

// run parses args and executes the requested HTTP operation, returning the
// process exit code. All work happens here (rather than directly in main)
// so that deferred cleanup - closing the client's idle connections and
// flushing the cookie jar to disk - always runs via a normal function
// return, on every exit path (errors, non-2xx status, download/upload
// completion), instead of being skipped by a direct os.Exit call.
func run(args []string) int {
	var (
		headers     multiFlag
		queryParams multiFlag
		formFields  multiFlag
	)

	fs := flag.NewFlagSet("relay", flag.ContinueOnError)

	// ── Request ──────────────────────────────────────────────────────────────
	method := fs.String("X", "GET", "HTTP `method` (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS)")
	data := fs.String("d", "", "request body; prefix with @ to read file, @- for stdin")
	jsonBody := fs.String("j", "", "JSON request body (sets Content-Type: application/json)")
	userAgent := fs.String("A", "", "User-Agent `string`")

	// ── Auth ─────────────────────────────────────────────────────────────────
	user := fs.String("u", "", "basic auth `user:password`")
	token := fs.String("t", "", "bearer `token`")
	apiKey := fs.String("k", "", "API key as `Header=value` (e.g. X-API-Key=secret)")
	cookies := fs.String("b", "", "send cookies as `Name=Value; Name2=Value2`")
	cookieJar := fs.String("c", "", "Netscape cookie `file` to load and save cookies")

	// ── Network ──────────────────────────────────────────────────────────────
	timeout := fs.Duration("timeout", 0, "max transfer `duration` (0 = no limit; Ctrl+C always cancels)")
	connectTimeout := fs.Duration("connect-timeout", 30*time.Second, "TCP/TLS connection `timeout`")
	maxRedir := fs.Int("L", 10, "maximum redirects (0 = disabled)")
	proxyURL := fs.String("proxy", "", "HTTP/HTTPS proxy URL")
	insecure := fs.Bool("insecure", false, "skip TLS certificate verification")

	// ── Retry / resilience ───────────────────────────────────────────────────
	retryMax := fs.Int("retry", 0, "maximum retry `attempts` (0 = disabled)")
	retryDelay := fs.Duration("retry-delay", 100*time.Millisecond, "initial retry interval")
	retryLog := fs.Bool("retry-verbose", false, "print each retry attempt to stderr")
	rateLimit := fs.Float64("rate", 0, "requests per second limit (0 = unlimited)")
	cbOn := fs.Bool("cb", false, "enable circuit breaker")
	cbFail := fs.Int("cb-failures", 5, "circuit-breaker consecutive-failure threshold")

	// ── Output / download ────────────────────────────────────────────────────
	outFile := fs.String("o", "", "write response body to `file` instead of stdout")
	remoteName := fs.Bool("O", false, "use remote filename (from URL or Content-Disposition)")
	resume := fs.Bool("C", false, "resume a partial download (-o or -O required)")
	parallel := fs.Int("P", 1, "max parallel downloads when multiple URLs are given")
	uploadFilePath := fs.String("upload-file", "", "upload `file` using PUT with a progress bar")
	dumpHdr := fs.String("D", "", "write response headers to `file`")
	headOnly := fs.Bool("I", false, "HEAD request only (prints headers to stdout)")
	pretty := fs.Bool("pretty", false, "pretty-print JSON response body")
	silent := fs.Bool("s", false, "suppress all output (exit code reflects HTTP status)")
	showTiming := fs.Bool("timing", false, "print per-phase timing breakdown to stderr")
	verbose := fs.Bool("v", false, "print request/response headers to stderr")
	include := fs.Bool("i", false, "include response headers in stdout output")
	noProgress := fs.Bool("no-progress", false, "disable download/upload progress bar")

	showVersion := fs.Bool("version", false, "print version and exit")

	fs.Var(&headers, "H", "add request `header` as \"Key: Value\" (repeatable)")
	fs.Var(&queryParams, "q", "add query `param` as \"key=value\" (repeatable)")
	fs.Var(&formFields, "F", "add form `field` as \"key=value\" (repeatable, multipart body)")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "relay %s - HTTP client powered by the relay library\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  relay [OPTIONS] <URL> [URL...]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  relay https://httpbin.org/get --timing --pretty\n")
		fmt.Fprintf(os.Stderr, "  relay -X POST -j '{\"name\":\"Alice\"}' --retry 3 https://api.example.com/users\n")
		fmt.Fprintf(os.Stderr, "  relay -O https://example.com/archive.zip                # download with progress\n")
		fmt.Fprintf(os.Stderr, "  relay -O -C https://example.com/big.iso                 # resume download\n")
		fmt.Fprintf(os.Stderr, "  relay -O -P 4 https://cdn.example.com/a.zip https://cdn.example.com/b.zip\n")
		fmt.Fprintf(os.Stderr, "  relay --upload-file firmware.bin https://api.example.com/upload\n")
		fmt.Fprintf(os.Stderr, "  relay -u admin:secret -I https://api.example.com/me     # HEAD + basic auth\n")
		fmt.Fprintf(os.Stderr, "  relay -b 'session=abc' --cb --rate 50 https://api.example.com/v1\n")
		fmt.Fprintf(os.Stderr, "  relay -c cookies.txt https://example.com/login          # cookie jar\n")
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	if *showVersion {
		fmt.Printf("relay %s\n", version)
		return 0
	}

	positional := fs.Args()
	if len(positional) == 0 {
		fs.Usage()
		return 1
	}

	if *headOnly {
		*method = "HEAD"
	}

	if *method == "GET" && (*data != "" || *jsonBody != "" || len(formFields) > 0) {
		*method = "POST"
	}

	opts, jar := buildOptions(
		*timeout, *connectTimeout, *maxRedir, *proxyURL, *insecure,
		*retryMax, *retryDelay, *retryLog,
		*rateLimit, *cbOn, *cbFail,
		*user, *token, *apiKey, *cookies, *cookieJar,
		*verbose, *silent,
	)
	if *showTiming {
		opts = append(opts, relay.WithTiming())
	}

	client := relay.New(opts...)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	defer func() { _ = client.Shutdown(context.Background()) }()
	if jar != nil {
		defer func() { _ = jar.Save() }()
	}

	quiet := *silent || *noProgress

	// ── Upload mode ──────────────────────────────────────────────────────────
	if *uploadFilePath != "" {
		resp, err := uploadFile(ctx, client, positional[0], *uploadFilePath, quiet)
		if err != nil {
			fmt.Fprintf(os.Stderr, "upload error: %v\n", err)
			return 1
		}
		if !*silent {
			writeResponse(resp, *include || *headOnly, *pretty, *showTiming, *dumpHdr)
		}
		return exitForStatus(resp.StatusCode)
	}

	dlCfg := downloadConfig{
		outPath:     *outFile,
		remoteNames: *remoteName,
		resume:      *resume,
		quiet:       quiet,
		parallel:    *parallel,
	}

	// ── Multi-URL download mode - only when -O is explicitly set ────────────
	if *remoteName {
		if err := downloadAll(ctx, client, positional, dlCfg); err != nil {
			fmt.Fprintf(os.Stderr, "download error: %v\n", err)
			return 1
		}
		return 0
	}

	// ── Single request, streaming to file ───────────────────────────────────
	if *outFile != "" {
		if err := downloadOne(ctx, client, positional[0], dlCfg); err != nil {
			fmt.Fprintf(os.Stderr, "download error: %v\n", err)
			return 1
		}
		return 0
	}

	// ── Normal single request ────────────────────────────────────────────────
	req, err := buildRequest(client, *method, positional[0], headers, queryParams, formFields, *data, *jsonBody, *userAgent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	resp, err := client.Execute(req.WithContext(ctx))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	if !*silent {
		writeResponse(resp, *include || *headOnly, *pretty, *showTiming, *dumpHdr)
	}

	return exitForStatus(resp.StatusCode)
}

// buildOptions assembles relay.Option values from the parsed flags.
func buildOptions(
	timeout, connectTimeout time.Duration, maxRedirects int, proxyURL string, insecure bool,
	retryMax int, retryInterval time.Duration, retryVerbose bool,
	rateLimit float64, cbEnable bool, cbFailures int,
	user, token, apiKey, cookies, cookieJarPath string,
	verbose, silent bool,
) ([]relay.Option, *fileCookieJar) {
	opts := []relay.Option{
		relay.WithTimeout(timeout),
		relay.WithDialTimeout(connectTimeout),
		relay.WithResponseHeaderTimeout(connectTimeout),
		relay.WithMaxRedirects(maxRedirects),
	}

	if proxyURL != "" {
		opts = append(opts, relay.WithProxy(proxyURL))
	}
	if insecure {
		opts = append(opts, relay.WithTLSConfig(&tls.Config{InsecureSkipVerify: true})) // #nosec G402
	}

	if retryMax > 0 {
		opts = append(opts, relay.WithRetry(buildRetryConfig(retryMax, retryInterval, retryVerbose)))
	}

	if rateLimit > 0 {
		burst := int(rateLimit)
		if burst < 1 {
			burst = 1
		}
		opts = append(opts, relay.WithRateLimit(rateLimit, burst+1))
	}

	if cbEnable {
		opts = append(opts, relay.WithCircuitBreaker(buildCircuitBreakerConfig(cbFailures)))
	}

	if defHeaders := buildAuthHeaders(user, token, apiKey, cookies); len(defHeaders) > 0 {
		opts = append(opts, relay.WithDefaultHeaders(defHeaders))
	}

	var jar *fileCookieJar
	if cookieJarPath != "" {
		var err error
		jar, err = newFileCookieJar(cookieJarPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: cookie jar: %v\n", err)
		} else {
			opts = append(opts, relay.WithCookieJar(jar))
		}
	}

	if verbose && !silent {
		opts = append(opts, buildVerboseHookOptions()...)
	}

	return opts, jar
}

// buildRetryConfig assembles the *relay.RetryConfig for -retry / -retry-delay
// / -retry-verbose.
func buildRetryConfig(retryMax int, retryInterval time.Duration, retryVerbose bool) *relay.RetryConfig {
	rc := &relay.RetryConfig{
		MaxAttempts:     retryMax + 1,
		InitialInterval: retryInterval,
		MaxInterval:     30 * time.Second,
		Multiplier:      2.0,
		RandomFactor:    0.3,
	}
	if retryVerbose {
		rc.OnRetry = func(attempt int, resp *http.Response, err error) {
			if err != nil {
				fmt.Fprintf(os.Stderr, "retry #%d: %v\n", attempt, err)
			} else {
				fmt.Fprintf(os.Stderr, "retry #%d: HTTP %d\n", attempt, resp.StatusCode)
			}
		}
	}
	return rc
}

// buildCircuitBreakerConfig assembles the *relay.CircuitBreakerConfig for -cb
// / -cb-failures.
func buildCircuitBreakerConfig(cbFailures int) *relay.CircuitBreakerConfig {
	return &relay.CircuitBreakerConfig{
		MaxFailures:      cbFailures,
		ResetTimeout:     10 * time.Second,
		HalfOpenRequests: 2,
		SuccessThreshold: 1,
		OnStateChange: func(from, to relay.CircuitBreakerState) {
			fmt.Fprintf(os.Stderr, "circuit breaker: %s → %s\n", from, to)
		},
	}
}

// buildAuthHeaders assembles the default headers driven by -u / -t / -k / -b.
func buildAuthHeaders(user, token, apiKey, cookies string) map[string]string {
	defHeaders := map[string]string{}
	if user != "" {
		username, password, _ := strings.Cut(user, ":")
		creds := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		defHeaders["Authorization"] = "Basic " + creds
	}
	if token != "" {
		defHeaders["Authorization"] = "Bearer " + token
	}
	if apiKey != "" {
		if h, v, ok := strings.Cut(apiKey, "="); ok {
			defHeaders[h] = v
		}
	}
	if cookies != "" {
		defHeaders["Cookie"] = cookies
	}
	return defHeaders
}

// buildVerboseHookOptions returns the before-request/after-response hooks
// used to print "> "/"< " trace lines for -v.
func buildVerboseHookOptions() []relay.Option {
	return []relay.Option{
		relay.WithOnBeforeRequest(func(_ context.Context, r *relay.Request) error {
			fmt.Fprintf(os.Stderr, "> %s %s\n", r.Method(), r.URL())
			return nil
		}),
		relay.WithOnAfterResponse(func(_ context.Context, r *relay.Response) error {
			fmt.Fprintf(os.Stderr, "< %s\n", r.Status)
			for _, k := range sortedKeys(r.Headers) {
				for _, v := range r.Headers[k] {
					fmt.Fprintf(os.Stderr, "< %s: %s\n", k, v)
				}
			}
			fmt.Fprintln(os.Stderr)
			return nil
		}),
	}
}

// buildRequest constructs a *relay.Request from the parsed CLI flags. It
// returns an error instead of exiting the process directly, so the caller
// can decide how to report it and still run deferred cleanup.
func buildRequest(
	client *relay.Client,
	method, rawURL string,
	headers, queryParams, formFields multiFlag,
	data, jsonBody, userAgent string,
) (*relay.Request, error) {
	var req *relay.Request
	switch strings.ToUpper(method) {
	case "POST":
		req = client.Post(rawURL)
	case "PUT":
		req = client.Put(rawURL)
	case "PATCH":
		req = client.Patch(rawURL)
	case "DELETE":
		req = client.Delete(rawURL)
	case "HEAD":
		req = client.Head(rawURL)
	case "OPTIONS":
		req = client.Options(rawURL)
	default:
		req = client.Get(rawURL)
	}

	if userAgent != "" {
		req = req.WithUserAgent(userAgent)
	}

	for _, h := range headers {
		if k, v, ok := strings.Cut(h, ":"); ok {
			req = req.WithHeader(strings.TrimSpace(k), strings.TrimSpace(v))
		}
	}
	for _, qp := range queryParams {
		if k, v, ok := strings.Cut(qp, "="); ok {
			req = req.WithQueryParam(k, v)
		}
	}

	switch {
	case jsonBody != "":
		body, err := readBody(jsonBody)
		if err != nil {
			return nil, fmt.Errorf("reading JSON body: %w", err)
		}
		var v json.RawMessage
		if err = json.Unmarshal(body, &v); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		req = req.WithJSON(v)

	case len(formFields) > 0:
		fields := make(map[string]string, len(formFields))
		for _, ff := range formFields {
			if k, v, ok := strings.Cut(ff, "="); ok {
				fields[k] = v
			}
		}
		req = req.WithFormData(fields)

	case data != "":
		body, err := readBody(data)
		if err != nil {
			return nil, fmt.Errorf("reading body: %w", err)
		}
		req = req.WithBody(body)
	}

	return req, nil
}

// writeResponse writes the response to stdout and optional meta to stderr.
func writeResponse(resp *relay.Response, includeHeaders, pretty bool, showTiming bool, dumpHdr string) {
	if dumpHdr != "" {
		if err := writeHeadersFile(resp, dumpHdr); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not write headers file: %v\n", err)
		}
	}

	if includeHeaders {
		fmt.Printf("HTTP/1.1 %s\n", resp.Status)
		for _, k := range sortedKeys(resp.Headers) {
			for _, v := range resp.Headers[k] {
				fmt.Printf("%s: %s\n", k, v)
			}
		}
		fmt.Println()
	}

	body := resp.Body()

	if pretty {
		var v interface{}
		if err := json.Unmarshal(body, &v); err == nil {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(v)
			goto timing
		}
	}
	_, _ = os.Stdout.Write(body)

timing:
	if showTiming {
		t := resp.Timing
		fmt.Fprintln(os.Stderr, "\n── Timing ──────────────────────────")
		printTimingRow("DNS lookup", t.DNSLookup)
		printTimingRow("TCP connect", t.TCPConnect)
		printTimingRow("TLS handshake", t.TLSHandshake)
		printTimingRow("Time to first byte", t.TimeToFirstByte)
		fmt.Fprintf(os.Stderr, "  %-22s %v\n", "Total", t.Total)
		fmt.Fprintln(os.Stderr, "────────────────────────────────────")
	}
}

func writeHeadersFile(resp *relay.Response, path string) error {
	f, err := os.Create(path) // #nosec G304
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "HTTP/1.1 %s\r\n", resp.Status)
	for _, k := range sortedKeys(resp.Headers) {
		for _, v := range resp.Headers[k] {
			_, _ = fmt.Fprintf(f, "%s: %s\r\n", k, v)
		}
	}
	_, _ = fmt.Fprint(f, "\r\n")
	return nil
}

func printTimingRow(label string, d time.Duration) {
	if d > 0 {
		fmt.Fprintf(os.Stderr, "  %-22s %v\n", label, d)
	}
}

// readBody reads body: "@file" from file, "@-" from stdin, otherwise literal bytes.
func readBody(s string) ([]byte, error) {
	if strings.HasPrefix(s, "@") {
		src := s[1:]
		if src == "-" {
			return io.ReadAll(os.Stdin)
		}
		f, err := os.Open(src) // #nosec G304
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		return io.ReadAll(f)
	}
	return []byte(s), nil
}

func sortedKeys(h http.Header) []string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// exitForStatus maps an HTTP response status to a process exit code: 5 for
// server errors, 4 for client errors, 0 otherwise.
func exitForStatus(code int) int {
	switch {
	case code >= 500:
		return 5
	case code >= 400:
		return 4
	}
	return 0
}
