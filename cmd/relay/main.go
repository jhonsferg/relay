// Package main implements a feature-rich HTTP client powered by the relay
// library, exposing retry, circuit-breaker, rate-limit, signing, and timing
// capabilities from the command line.
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

const version = "0.1.0"

// multiFlag accumulates repeated flag values (e.g. -H "K: V" -H "K2: V2").
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ", ") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	var (
		headers     multiFlag
		queryParams multiFlag
		formFields  multiFlag

		method      = flag.String("X", "GET", "HTTP `method` (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS)")
		data        = flag.String("d", "", "request body; prefix with @ to read from a file")
		jsonBody    = flag.String("j", "", "JSON request body (sets Content-Type: application/json)")
		user        = flag.String("u", "", "basic auth `user:password`")
		token       = flag.String("t", "", "bearer token")
		apiKey      = flag.String("k", "", "API key as `Header=value` (e.g. X-API-Key=secret)")
		timeout     = flag.Duration("timeout", 30*time.Second, "request timeout")
		maxRedirect = flag.Int("L", 10, "maximum redirects (0 = disabled)")
		proxyURL    = flag.String("proxy", "", "HTTP/HTTPS proxy URL")
		insecure    = flag.Bool("insecure", false, "skip TLS certificate verification")

		retryMax      = flag.Int("retry", 0, "maximum retry attempts (0 = disabled)")
		retryInterval = flag.Duration("retry-delay", 100*time.Millisecond, "initial retry interval")
		retryVerbose  = flag.Bool("retry-verbose", false, "print each retry attempt to stderr")

		rateLimit = flag.Float64("rate", 0, "requests per second limit (0 = unlimited)")

		cbEnable   = flag.Bool("cb", false, "enable circuit breaker")
		cbFailures = flag.Int("cb-failures", 5, "circuit-breaker consecutive-failure threshold")

		verbose = flag.Bool("v", false, "print request and response headers to stderr")
		include = flag.Bool("i", false, "include response headers in stdout output")
		outFile = flag.String("o", "", "write response body to `file` instead of stdout")
		pretty  = flag.Bool("pretty", false, "pretty-print JSON response body")
		silent  = flag.Bool("s", false, "suppress all output (exit code reflects HTTP status)")
		timing  = flag.Bool("timing", false, "print per-phase timing breakdown to stderr")

		showVersion = flag.Bool("version", false, "print version and exit")
	)

	flag.Var(&headers, "H", "add request `header` as \"Key: Value\" (repeatable)")
	flag.Var(&queryParams, "q", "add query `param` as \"key=value\" (repeatable)")
	flag.Var(&formFields, "F", "add form `field` as \"key=value\" (repeatable, multipart body)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "relay %s — HTTP client powered by the relay library\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage:\n  relay [OPTIONS] <URL>\n  relay -X POST -j '{\"name\":\"Alice\"}' <URL>\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  relay https://httpbin.org/get\n")
		fmt.Fprintf(os.Stderr, "  relay -X POST -j '{\"name\":\"Alice\"}' https://httpbin.org/post\n")
		fmt.Fprintf(os.Stderr, "  relay -H 'Accept: application/json' -t mytoken https://api.example.com/me\n")
		fmt.Fprintf(os.Stderr, "  relay --retry 3 --timing https://httpbin.org/status/503\n")
		fmt.Fprintf(os.Stderr, "  relay --cb --rate 10 https://httpbin.org/get\n")
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

	rawURL := args[0]

	// Infer POST when a body flag is set but the method was not explicitly overridden.
	if *method == "GET" && (*data != "" || *jsonBody != "" || len(formFields) > 0) {
		*method = "POST"
	}

	opts := buildOptions(
		*timeout, *maxRedirect, *proxyURL, *insecure,
		*retryMax, *retryInterval, *retryVerbose,
		*rateLimit, *cbEnable, *cbFailures,
		*user, *token, *apiKey,
		*verbose, *silent,
	)

	client := relay.New(opts...)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	defer func() { _ = client.Shutdown(context.Background()) }()

	req := buildRequest(client, *method, rawURL, headers, queryParams, formFields, *data, *jsonBody)

	resp, err := client.Execute(req.WithContext(ctx))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if !*silent {
		writeResponse(resp, *include, *pretty, *outFile, *timing)
	}

	switch {
	case resp.StatusCode >= 500:
		os.Exit(5)
	case resp.StatusCode >= 400:
		os.Exit(4)
	}
}

// buildOptions assembles relay.Option values from the parsed flags.
func buildOptions(
	timeout time.Duration, maxRedirects int, proxyURL string, insecure bool,
	retryMax int, retryInterval time.Duration, retryVerbose bool,
	rateLimit float64, cbEnable bool, cbFailures int,
	user, token, apiKey string,
	verbose, silent bool,
) []relay.Option {
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
		if header, value, ok := strings.Cut(apiKey, "="); ok {
			defHeaders[header] = value
		}
	}
	if len(defHeaders) > 0 {
		opts = append(opts, relay.WithDefaultHeaders(defHeaders))
	}

	if verbose && !silent {
		opts = append(opts,
			relay.WithOnBeforeRequest(func(_ context.Context, r *relay.Request) error {
				fmt.Fprintf(os.Stderr, "> %s %s\n", r.Method(), r.URL())
				return nil
			}),
			relay.WithOnAfterResponse(func(_ context.Context, r *relay.Response) error {
				fmt.Fprintf(os.Stderr, "< %s\n", r.Status)
				keys := sortedKeys(r.Headers)
				for _, k := range keys {
					for _, v := range r.Headers[k] {
						fmt.Fprintf(os.Stderr, "< %s: %s\n", k, v)
					}
				}
				fmt.Fprintln(os.Stderr)
				return nil
			}),
		)
	}

	return opts
}

// buildRequest constructs a *relay.Request from the parsed CLI flags.
func buildRequest(
	client *relay.Client,
	method, rawURL string,
	headers, queryParams, formFields multiFlag,
	data, jsonBody string,
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

// writeResponse writes the response to stdout and optional timing to stderr.
func writeResponse(resp *relay.Response, includeHeaders, pretty bool, outFile string, showTiming bool) {
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

	switch {
	case outFile != "":
		if err := os.WriteFile(outFile, body, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error writing output file: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "saved %d bytes → %s\n", len(body), outFile)

	case pretty:
		var v interface{}
		if err := json.Unmarshal(body, &v); err == nil {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(v)
		} else {
			_, _ = os.Stdout.Write(body)
		}

	default:
		_, _ = os.Stdout.Write(body)
	}

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

func printTimingRow(label string, d time.Duration) {
	if d > 0 {
		fmt.Fprintf(os.Stderr, "  %-22s %v\n", label, d)
	}
}

// readBody reads a body value: "@path" reads from file, otherwise returns raw bytes.
func readBody(s string) ([]byte, error) {
	if strings.HasPrefix(s, "@") {
		f, err := os.Open(s[1:])
		if err != nil {
			return nil, err
		}
		defer f.Close()
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
