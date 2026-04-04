// Package main implements an HTTP load-testing tool powered by relay,
// demonstrating concurrent execution, connection pooling, rate limiting,
// and circuit-breaker observation.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jhonsferg/relay"
)

const version = "0.1.0"

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ", ") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	var headers multiFlag

	method    := flag.String("m", "GET", "HTTP `method`")
	body      := flag.String("b", "", "request body")
	jsonBody  := flag.String("j", "", "JSON request body (sets Content-Type: application/json)")
	count     := flag.Int("n", 100, "total number of requests (ignored when -d is set)")
	conc      := flag.Int("c", 10, "concurrency — simultaneous in-flight requests")
	dur       := flag.Duration("d", 0, "run duration (e.g. 30s); overrides -n when set")
	reqTout   := flag.Duration("timeout", 10*time.Second, "per-request timeout")
	rateLimit := flag.Float64("rate", 0, "requests per second limit (0 = unlimited)")
	cbEnable  := flag.Bool("cb", false, "enable circuit breaker (trips at 10 consecutive failures)")
	warm      := flag.Int("warm", 0, "warm-up requests sent before measurement (results discarded)")
	jsonOut   := flag.Bool("json", false, "output results as JSON")
	quiet     := flag.Bool("q", false, "suppress progress output during benchmark")
	showVer   := flag.Bool("version", false, "print version and exit")

	flag.Var(&headers, "H", "add request `header` as \"Key: Value\" (repeatable)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "relay-bench %s — HTTP load tester powered by relay\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage:\n  relay-bench [OPTIONS] <URL>\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  relay-bench -n 500 -c 25 https://httpbin.org/get\n")
		fmt.Fprintf(os.Stderr, "  relay-bench -d 30s -c 50 --rate 200 https://api.example.com/ping\n")
		fmt.Fprintf(os.Stderr, "  relay-bench -n 1000 -c 100 --cb --json https://api.example.com/v1\n")
	}

	flag.Parse()

	if *showVer {
		fmt.Printf("relay-bench %s\n", version)
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	rawURL := args[0]

	opts := []relay.Option{
		relay.WithTimeout(*reqTout),
		relay.WithConnectionPool(*conc+10, *conc, *conc+10),
		relay.WithDisableRetry(),
	}

	if *rateLimit > 0 {
		burst := int(*rateLimit)
		if burst < 1 {
			burst = 1
		}
		opts = append(opts, relay.WithRateLimit(*rateLimit, burst))
	}

	var cbTransitions []string
	if *cbEnable {
		opts = append(opts, relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
			MaxFailures:      10,
			ResetTimeout:     5 * time.Second,
			HalfOpenRequests: 3,
			SuccessThreshold: 2,
			OnStateChange: func(from, to relay.CircuitBreakerState) {
				cbTransitions = append(cbTransitions, fmt.Sprintf("%s→%s", from, to))
				if !*quiet {
					fmt.Fprintf(os.Stderr, "\rcircuit breaker: %s → %s\n", from, to)
				}
			},
		}))
	}

	client := relay.New(opts...)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	defer func() { _ = client.Shutdown(context.Background()) }()

	reqFactory := makeFactory(client, strings.ToUpper(*method), rawURL, headers, *body, *jsonBody)

	// Warm-up phase — results are discarded so the connection pool is primed.
	if *warm > 0 {
		if !*quiet {
			fmt.Fprintf(os.Stderr, "warming up with %d requests…\n", *warm)
		}
		warmReqs := make([]*relay.Request, *warm)
		for i := range warmReqs {
			warmReqs[i] = reqFactory()
		}
		client.ExecuteBatch(ctx, warmReqs, *conc)
	}

	if !*quiet {
		if *dur > 0 {
			fmt.Fprintf(os.Stderr, "benchmarking %s for %s with concurrency %d…\n", rawURL, *dur, *conc)
		} else {
			fmt.Fprintf(os.Stderr, "benchmarking %s: %d requests, concurrency %d…\n", rawURL, *count, *conc)
		}
	}

	var stats *Stats
	if *dur > 0 {
		stats = runDuration(ctx, client, reqFactory, *conc, *dur, *quiet)
	} else {
		stats = runCount(ctx, client, reqFactory, *count, *conc, *quiet)
	}

	stats.CBTransitions = cbTransitions
	stats.URL = rawURL
	stats.Method = strings.ToUpper(*method)
	stats.Concurrency = *conc

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(stats)
	} else {
		printStats(stats)
	}

	if stats.Errors > 0 || stats.Failures > 0 {
		os.Exit(1)
	}
}

// makeFactory returns a function that creates a fresh *relay.Request on each call.
func makeFactory(client *relay.Client, method, rawURL string, headers multiFlag, body, jsonBody string) func() *relay.Request {
	return func() *relay.Request {
		var req *relay.Request
		switch method {
		case "POST":
			req = client.Post(rawURL)
		case "PUT":
			req = client.Put(rawURL)
		case "PATCH":
			req = client.Patch(rawURL)
		case "DELETE":
			req = client.Delete(rawURL)
		default:
			req = client.Get(rawURL)
		}
		for _, h := range headers {
			if k, v, ok := strings.Cut(h, ":"); ok {
				req = req.WithHeader(strings.TrimSpace(k), strings.TrimSpace(v))
			}
		}
		switch {
		case jsonBody != "":
			req = req.WithJSON(json.RawMessage(jsonBody))
		case body != "":
			req = req.WithBody([]byte(body))
		}
		return req
	}
}
