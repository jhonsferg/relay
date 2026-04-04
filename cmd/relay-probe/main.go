// Package main implements a multi-endpoint health probe powered by relay,
// demonstrating retry, circuit-breaker, and periodic monitoring capabilities.
package main

import (
	"context"
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
	expect := flag.Int("expect", 200, "expected HTTP status code")
	timeout := flag.Duration("timeout", 10*time.Second, "per-request timeout")
	retries := flag.Int("retry", 2, "retry attempts on failure")
	interval := flag.Duration("interval", 0, "watch interval — 0 runs a single check")
	watchCnt := flag.Int("count", 0, "watch iterations (0 = unlimited, requires -interval)")
	maxLat := flag.Duration("latency", 0, "maximum acceptable latency (0 = no limit)")
	cbEnable := flag.Bool("cb", false, "enable circuit breaker per endpoint")
	verbose := flag.Bool("v", false, "verbose output")
	jsonOut := flag.Bool("json", false, "output results as JSON")
	showVer := flag.Bool("version", false, "print version and exit")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "relay-probe %s — health probe powered by relay\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage:\n  relay-probe [OPTIONS] <URL> [URL...]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
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

	flag.Parse()

	if *showVer {
		fmt.Printf("relay-probe %s\n", version)
		os.Exit(0)
	}

	urls := flag.Args()
	if len(urls) == 0 {
		flag.Usage()
		os.Exit(1)
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
		os.Exit(printReport(results, *jsonOut))
	}

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	for iteration := 1; ; iteration++ {
		results := runChecks(ctx, probes, cfg)
		code := printReport(results, *jsonOut)

		if *watchCnt > 0 && iteration >= *watchCnt {
			os.Exit(code)
		}

		select {
		case <-ctx.Done():
			os.Exit(0)
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
