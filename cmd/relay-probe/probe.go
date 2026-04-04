package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/jhonsferg/relay"
)

// checkConfig controls how each probe result is evaluated.
type checkConfig struct {
	expectedStatus int
	maxLatency     time.Duration
	verbose        bool
}

// CheckResult holds the outcome of checking a single endpoint.
type CheckResult struct {
	URL          string        `json:"url"`
	Healthy      bool          `json:"healthy"`
	StatusCode   int           `json:"status_code,omitempty"`
	Latency      time.Duration `json:"latency_ns,omitempty"`
	LatencyHuman string        `json:"latency,omitempty"`
	Error        string        `json:"error,omitempty"`
	Reason       string        `json:"reason,omitempty"`
	CBOpen       bool          `json:"circuit_breaker_open,omitempty"`
	CheckedAt    time.Time     `json:"checked_at"`
}

// runChecks executes health checks against all probes concurrently and
// returns one CheckResult per probe.
func runChecks(ctx context.Context, probes []*probe, cfg checkConfig) []CheckResult {
	results := make([]CheckResult, len(probes))
	var wg sync.WaitGroup

	for i, p := range probes {
		wg.Add(1)
		go func(idx int, p *probe) {
			defer wg.Done()
			results[idx] = check(ctx, p, cfg)
		}(i, p)
	}

	wg.Wait()
	return results
}

// check performs a single GET against the probe endpoint and evaluates it.
func check(ctx context.Context, p *probe, cfg checkConfig) CheckResult {
	r := CheckResult{
		URL:       p.url,
		CheckedAt: time.Now().UTC(),
	}

	if !p.client.IsHealthy() {
		r.CBOpen = true
		r.Reason = "circuit breaker open"
		if cfg.verbose {
			fmt.Fprintf(os.Stderr, "  [%s] circuit breaker open - skipping\n", p.url)
		}
		return r
	}

	resp, err := p.client.Execute(p.client.Get(p.url).WithContext(ctx))
	if err != nil {
		r.Error = err.Error()
		r.Reason = "request failed"
		if cfg.verbose {
			fmt.Fprintf(os.Stderr, "  [%s] error: %v\n", p.url, err)
		}
		return r
	}

	r.StatusCode = resp.StatusCode
	r.Latency = resp.Timing.Total
	r.LatencyHuman = resp.Timing.Total.Round(time.Microsecond).String()
	relay.PutResponse(resp)

	switch {
	case r.StatusCode != cfg.expectedStatus:
		r.Reason = fmt.Sprintf("expected HTTP %d, got %d", cfg.expectedStatus, r.StatusCode)

	case cfg.maxLatency > 0 && r.Latency > cfg.maxLatency:
		r.Reason = fmt.Sprintf("latency %s exceeds threshold %s",
			r.Latency.Round(time.Millisecond), cfg.maxLatency)

	default:
		r.Healthy = true
	}

	if cfg.verbose {
		icon := "✓"
		if !r.Healthy {
			icon = "✗"
		}
		fmt.Fprintf(os.Stderr, "  %s [%s] HTTP %d  %s\n", icon, p.url, r.StatusCode, r.LatencyHuman)
		if r.Reason != "" {
			fmt.Fprintf(os.Stderr, "    reason: %s\n", r.Reason)
		}
	}

	return r
}

// printReport writes the check results and returns the appropriate exit code.
// Exit codes: 0 = all healthy, 1 = unhealthy, 2 = circuit breaker open.
func printReport(results []CheckResult, asJSON bool) int {
	exitCode := 0

	for _, r := range results {
		if r.CBOpen {
			if exitCode < 2 {
				exitCode = 2
			}
		} else if !r.Healthy {
			if exitCode < 1 {
				exitCode = 1
			}
		}
	}

	if asJSON {
		report := struct {
			Timestamp time.Time     `json:"timestamp"`
			Healthy   bool          `json:"healthy"`
			Results   []CheckResult `json:"results"`
		}{
			Timestamp: time.Now().UTC(),
			Healthy:   exitCode == 0,
			Results:   results,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
		return exitCode
	}

	ts := time.Now().Format("15:04:05")
	fmt.Printf("\n[%s] Health check - %d endpoint(s)\n", ts, len(results))
	fmt.Println("──────────────────────────────────────────────────")

	for _, r := range results {
		switch {
		case r.CBOpen:
			fmt.Printf("  ⊘ %-40s  circuit breaker OPEN\n", truncate(r.URL, 40))
		case !r.Healthy:
			detail := r.Error
			if detail == "" {
				detail = r.Reason
			}
			fmt.Printf("  ✗ %-40s  %s\n", truncate(r.URL, 40), detail)
		default:
			fmt.Printf("  ✓ %-40s  HTTP %d  %s\n", truncate(r.URL, 40), r.StatusCode, r.LatencyHuman)
		}
	}

	fmt.Println("──────────────────────────────────────────────────")
	status := "HEALTHY"
	if exitCode > 0 {
		status = "UNHEALTHY"
	}
	fmt.Printf("  Result: %s\n\n", status)

	return exitCode
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
