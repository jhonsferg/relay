// Package main implements a multi-endpoint health probe powered by relay,
// demonstrating retry, circuit-breaker, and periodic monitoring capabilities.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jhonsferg/relay"
)

const version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

// run parses args and executes the health probe, returning the process exit
// code. All work happens here (rather than directly in main) so deferred
// cleanup - shutting down every probe's connection pool - always runs via a
// normal function return instead of being skipped by a direct os.Exit call.
func run(args []string) int {
	fs := flag.NewFlagSet("relay-probe", flag.ContinueOnError)

	expect := fs.Int("expect", 200, "expected HTTP status code")
	timeout := fs.Duration("timeout", 10*time.Second, "per-request timeout")
	retries := fs.Int("retry", 2, "retry attempts on failure")
	interval := fs.Duration("interval", 0, "watch interval - 0 runs a single check")
	watchCnt := fs.Int("count", 0, "watch iterations (0 = unlimited, requires -interval)")
	maxLat := fs.Duration("latency", 0, "maximum acceptable latency (0 = no limit)")
	cbEnable := fs.Bool("cb", false, "enable circuit breaker per endpoint")
	verbose := fs.Bool("v", false, "verbose output")
	jsonOut := fs.Bool("json", false, "output results as JSON")
	showVer := fs.Bool("version", false, "print version and exit")

	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "relay-probe %s - health probe powered by relay\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage:\n  relay-probe [OPTIONS] <URL> [URL...]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExit codes:\n")
		fmt.Fprintf(os.Stderr, "  0  all endpoints healthy\n")
		fmt.Fprintf(os.Stderr, "  1  one or more endpoints unhealthy\n")
		fmt.Fprintf(os.Stderr, "  2  one or more circuit breakers open\n")
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  relay-probe https://api.example.com/health\n")
		fmt.Fprintf(os.Stderr, "  relay-probe --retry 3 --latency 500ms https://api.example.com/health\n")
		fmt.Fprintf(os.Stderr, "  relay-probe --interval 30s --count 10 https://svc1/health https://svc2/health\n")
		fmt.Fprintf(os.Stderr, "  relay-probe --cb --json https://api.example.com/health\n")
	}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	if *showVer {
		fmt.Printf("relay-probe %s\n", version)
		return 0
	}

	urls := fs.Args()
	if len(urls) == 0 {
		fs.Usage()
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	probes := buildProbes(urls, *timeout, *retries, *cbEnable, *verbose)
	defer shutdownAll(probes)

	cfg := checkConfig{
		expectedStatus: *expect,
		maxLatency:     *maxLat,
		verbose:        *verbose,
	}

	if *interval <= 0 {
		results := runChecks(ctx, probes, cfg)
		return printReport(results, *jsonOut)
	}

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for iteration := 1; ; iteration++ {
		results := runChecks(ctx, probes, cfg)
		code := printReport(results, *jsonOut)

		if *watchCnt > 0 && iteration >= *watchCnt {
			return code
		}

		select {
		case <-ctx.Done():
			return 0
		case <-ticker.C:
		}
	}
}

// probe holds a relay.Client scoped to a single endpoint URL.
type probe struct {
	url    string
	client *relay.Client
}

func buildProbes(urls []string, timeout time.Duration, retries int, cbEnable, verbose bool) []*probe {
	probes := make([]*probe, len(urls))
	for i, u := range urls {
		probes[i] = &probe{url: u, client: newProbeClient(u, timeout, retries, cbEnable, verbose)}
	}
	return probes
}

func newProbeClient(url string, timeout time.Duration, retries int, cbEnable, verbose bool) *relay.Client {
	opts := []relay.Option{
		relay.WithTimeout(timeout),
		relay.WithTiming(), // probe always reports per-request latency
	}

	if retries > 0 {
		rc := &relay.RetryConfig{
			MaxAttempts:     retries + 1,
			InitialInterval: 200 * time.Millisecond,
			MaxInterval:     5 * time.Second,
			Multiplier:      2.0,
			RandomFactor:    0.2,
		}
		if verbose {
			rc.OnRetry = func(attempt int, resp *http.Response, err error) {
				if err != nil {
					fmt.Fprintf(os.Stderr, "  [%s] retry #%d: %v\n", url, attempt, err)
				} else {
					fmt.Fprintf(os.Stderr, "  [%s] retry #%d: HTTP %d\n", url, attempt, resp.StatusCode)
				}
			}
		}
		opts = append(opts, relay.WithRetry(rc))
	} else {
		opts = append(opts, relay.WithDisableRetry())
	}

	if cbEnable {
		opts = append(opts, relay.WithCircuitBreaker(&relay.CircuitBreakerConfig{
			MaxFailures:      3,
			ResetTimeout:     30 * time.Second,
			HalfOpenRequests: 1,
			SuccessThreshold: 1,
			OnStateChange: func(from, to relay.CircuitBreakerState) {
				fmt.Fprintf(os.Stderr, "  [%s] circuit breaker: %s → %s\n", url, from, to)
			},
		}))
	}

	return relay.New(opts...)
}

func shutdownAll(probes []*probe) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, p := range probes {
		_ = p.client.Shutdown(ctx)
	}
}
