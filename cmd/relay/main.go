// Package main implements a feature-rich HTTP client powered by the relay
// library, exposing retry, circuit-breaker, rate-limit, signing, streaming,
// download/upload and timing capabilities from the command line.
package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
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
	var (
		headers     multiFlag
		queryParams multiFlag
		formFields  multiFlag
	)

	// ── Request ──────────────────────────────────────────────────────────────
	method := flag.String("X", "GET", "HTTP `method` (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS)")
	data := flag.String("d", "", "request body; prefix with @ to read file, @- for stdin")
	jsonBody := flag.String("j", "", "JSON request body (sets Content-Type: application/json)")
	userAgent := flag.String("A", "", "User-Agent `string`")

	// ── Auth ─────────────────────────────────────────────────────────────────
	user := flag.String("u", "", "basic auth `user:password`")
	token := flag.String("t", "", "bearer `token`")
	apiKey := flag.String("k", "", "API key as `Header=value` (e.g. X-API-Key=secret)")
	cookies := flag.String("b", "", "send cookies as `Name=Value; Name2=Value2`")
	cookieJar := flag.String("c", "", "Netscape cookie `file` to load and save cookies")

	// ── Network ──────────────────────────────────────────────────────────────
	timeout := flag.Duration("timeout", 30*time.Second, "request timeout")
	maxRedir := flag.Int("L", 10, "maximum redirects (0 = disabled)")
	proxyURL := flag.String("proxy", "", "HTTP/HTTPS proxy URL")
	insecure := flag.Bool("insecure", false, "skip TLS certificate verification")

	// ── Retry / resilience ───────────────────────────────────────────────────
	retryMax := flag.Int("retry", 0, "maximum retry `attempts` (0 = disabled)")
	retryDelay := flag.Duration("retry-delay", 100*time.Millisecond, "initial retry interval")
	retryLog := flag.Bool("retry-verbose", false, "print each retry attempt to stderr")
	rateLimit := flag.Float64("rate", 0, "requests per second limit (0 = unlimited)")
	cbOn := flag.Bool("cb", false, "enable circuit breaker")
	cbFail := flag.Int("cb-failures", 5, "circuit-breaker consecutive-failure threshold")

	// ── Output / download ────────────────────────────────────────────────────
	outFile := flag.String("o", "", "write response body to `file` instead of stdout")
	remoteName := flag.Bool("O", false, "use remote filename (from URL or Content-Disposition)")
	resume := flag.Bool("C", false, "resume a partial download (-o or -O required)")
	parallel := flag.Int("P", 1, "max parallel downloads when multiple URLs are given")
	uploadFilePath := flag.String("upload-file", "", "upload `file` using PUT with a progress bar")
	dumpHdr := flag.String("D", "", "write response headers to `file`")
	headOnly := flag.Bool("I", false, "HEAD request only (prints headers to stdout)")
	pretty := flag.Bool("pretty", false, "pretty-print JSON response body")
	silent := flag.Bool("s", false, "suppress all output (exit code reflects HTTP status)")
	showTiming := flag.Bool("timing", false, "print per-phase timing breakdown to stderr")
	verbose := flag.Bool("v", false, "print request/response headers to stderr")
	include := flag.Bool("i", false, "include response headers in stdout output")
	noProgress := flag.Bool("no-progress", false, "disable download/upload progress bar")

	showVersion := flag.Bool("version", false, "print version and exit")

	flag.Var(&headers, "H", "add request `header` as \"Key: Value\" (repeatable)")
	flag.Var(&queryParams, "q", "add query `param` as \"key=value\" (repeatable)")
	flag.Var(&formFields, "F", "add form `field` as \"key=value\" (repeatable, multipart body)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "relay %s — HTTP client powered by the relay library\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  relay [OPTIONS] <URL> [URL...]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
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

	flag.Parse()

	if *showVersion {
		fmt.Printf("relay %s\n", version)
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	if *headOnly {
		*method = "HEAD"
	}

	if *method == "GET" && (*data != "" || *jsonBody != "" || len(formFields) > 0) {
		*method = "POST"
	}

	opts, jar := buildOptions(
		*timeout, *maxRedir, *proxyURL, *insecure,
		*retryMax, *retryDelay, *retryLog,
		*rateLimit, *cbOn, *cbFail,
		*user, *token, *apiKey, *cookies, *cookieJar,
		*verbose, *silent,
	)

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
		resp, err := uploadFile(ctx, client, args[0], *uploadFilePath, quiet)
		if err != nil {
			fmt.Fprintf(os.Stderr, "upload error: %v\n", err)
			os.Exit(1)
		}
		if !*silent {
			writeResponse(resp, *include || *headOnly, *pretty, "", *showTiming, *dumpHdr)
		}
		exitForStatus(resp.StatusCode)
		return
	}

	dlCfg := downloadConfig{
		outPath:     *outFile,
		remoteNames: *remoteName,
		resume:      *resume,
		quiet:       quiet,
		parallel:    *parallel,
	}

	// ── Multi-URL download mode — only when -O is explicitly set ────────────
	if *remoteName {
		if err := downloadAll(ctx, client, args, dlCfg); err != nil {
			fmt.Fprintf(os.Stderr, "download error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// ── Single request, streaming to file ───────────────────────────────────
	if *outFile != "" {
		if err := downloadOne(ctx, client, args[0], dlCfg); err != nil {
			fmt.Fprintf(os.Stderr, "download error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// ── Normal single request ────────────────────────────────────────────────
	req := buildRequest(client, *method, args[0], headers, queryParams, formFields, *data, *jsonBody, *userAgent)

	resp, err := client.Execute(req.WithContext(ctx))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if !*silent {
		writeResponse(resp, *include || *headOnly, *pretty, "", *showTiming, *dumpHdr)
	}

	exitForStatus(resp.StatusCode)
}

// buildOptions assembles relay.Option values from the parsed flags.
func buildOptions(
	timeout time.Duration, maxRedirects int, proxyURL string, insecure bool,
	retryMax int, retryInterval time.Duration, retryVerbose bool,
	rateLimit float64, cbEnable bool, cbFailures int,
	user, token, apiKey, cookies, cookieJarPath string,
	verbose, silent bool,
) ([]relay.Option, *fileCookieJar) {
	opts := []relay.Option{
		relay.WithTimeout(timeout),
		relay.WithMaxRedirects(maxRedirects),
	}

	if proxyURL != "" {
		opts = append(opts, relay.WithProxy(proxyURL))
	}
	if insecure {
		opts = append(opts, relay.WithTLSConfig(&tls.Config{InsecureSkipVerify: true})) // #nosec G402
	}

	if retryMax > 0 {
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
		opts = append(opts, relay.WithRetry(rc))
	}

	if rateLimit > 0 {
		burst := int(rateLimit)
		if burst < 1 {
			burst = 1
		}
		opts = append(opts, relay.WithRateLimit(rateLimit, burst+1))
	}

	if cbEnable {
		opts = append(opts, relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
			MaxFailures:      cbFailures,
			ResetTimeout:     10 * time.Second,
			HalfOpenRequests: 2,
			SuccessThreshold: 1,
			OnStateChange: func(from, to relay.CircuitBreakerState) {
				fmt.Fprintf(os.Stderr, "circuit breaker: %s → %s\n", from, to)
			},
		}))
	}

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
	if len(defHeaders) > 0 {
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
		opts = append(opts,
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
		)
	}

	return opts, jar
}

// buildRequest constructs a *relay.Request from the parsed CLI flags.
func buildRequest(
	client *relay.Client,
	method, rawURL string,
	headers, queryParams, formFields multiFlag,
	data, jsonBody, userAgent string,
) *relay.Request {
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
			fmt.Fprintf(os.Stderr, "error reading JSON body: %v\n", err)
			os.Exit(1)
		}
		var v json.RawMessage
		if err = json.Unmarshal(body, &v); err != nil {
			fmt.Fprintf(os.Stderr, "invalid JSON: %v\n", err)
			os.Exit(1)
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
			fmt.Fprintf(os.Stderr, "error reading body: %v\n", err)
			os.Exit(1)
		}
		req = req.WithBody(body)
	}

	return req
}

// writeResponse writes the response to stdout and optional meta to stderr.
func writeResponse(resp *relay.Response, includeHeaders, pretty bool, _ string, showTiming bool, dumpHdr string) {
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

func exitForStatus(code int) {
	switch {
	case code >= 500:
		os.Exit(5)
	case code >= 400:
		os.Exit(4)
	}
}
