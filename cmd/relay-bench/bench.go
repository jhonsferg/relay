package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/jhonsferg/relay"
)

// result holds the outcome of a single request in a benchmark run.
type result struct {
	latency    time.Duration
	statusCode int
	err        error
}

// Stats holds aggregated results from a benchmark run.
type Stats struct {
	URL           string      `json:"url"`
	Method        string      `json:"method"`
	Concurrency   int         `json:"concurrency"`
	Total         int         `json:"total"`
	Successes     int         `json:"successes"`
	Failures      int         `json:"failures"`  // 4xx/5xx responses
	Errors        int         `json:"errors"`    // transport / dial errors
	Duration      duration    `json:"duration"`
	RPS           float64     `json:"requests_per_second"`
	LatencyMin    duration    `json:"latency_min"`
	LatencyMean   duration    `json:"latency_mean"`
	LatencyP50    duration    `json:"latency_p50"`
	LatencyP95    duration    `json:"latency_p95"`
	LatencyP99    duration    `json:"latency_p99"`
	LatencyMax    duration    `json:"latency_max"`
	StatusCodes   map[int]int `json:"status_codes"`
	CBTransitions []string    `json:"circuit_breaker_transitions,omitempty"`
}

// duration is a time.Duration that marshals to a human-readable string in JSON.
type duration time.Duration

func (d duration) MarshalJSON() ([]byte, error) {
	s := time.Duration(d).String()
	return []byte(`"` + s + `"`), nil
}

// runCount executes exactly n requests with the given concurrency using
// relay's ExecuteBatch, which manages the worker pool automatically.
func runCount(ctx context.Context, client *relay.Client, factory func() *relay.Request, n, conc int, quiet bool) *Stats {
	reqs := make([]*relay.Request, n)
	for i := range reqs {
		reqs[i] = factory()
	}

	start := time.Now()
	batchResults := client.ExecuteBatch(ctx, reqs, conc)
	elapsed := time.Since(start)

	if !quiet {
		fmt.Fprintln(os.Stderr)
	}

	results := make([]result, len(batchResults))
	for i, br := range batchResults {
		if br.Err != nil {
			results[i] = result{err: br.Err}
			continue
		}
		results[i] = result{
			latency:    br.Response.Timing.Total,
			statusCode: br.Response.StatusCode,
		}
		relay.PutResponse(br.Response)
	}

	return buildStats(results, elapsed)
}

// runDuration sends requests continuously for the given duration using a
// worker-pool built on relay.ExecuteAsync for fine-grained timing control.
func runDuration(ctx context.Context, client *relay.Client, factory func() *relay.Request, conc int, dur time.Duration, quiet bool) *Stats {
	dctx, cancel := context.WithTimeout(ctx, dur)
	defer cancel()

	var (
		mu      sync.Mutex
		results []result
		wg      sync.WaitGroup
	)

	sem := make(chan struct{}, conc)
	start := time.Now()

	for dctx.Err() == nil {
		select {
		case sem <- struct{}{}:
		case <-dctx.Done():
			break
		}
		if dctx.Err() != nil {
			<-sem
			break
		}

		wg.Add(1)
		req := factory().WithContext(dctx)
		reqStart := time.Now()
		ch := client.ExecuteAsync(req)

		go func(ch <-chan relay.AsyncResult, t time.Time) {
			defer wg.Done()
			defer func() { <-sem }()

			ar := <-ch
			lat := time.Since(t)

			r := result{latency: lat}
			if ar.Err != nil {
				r.err = ar.Err
			} else {
				r.statusCode = ar.Response.StatusCode
				relay.PutResponse(ar.Response)
			}

			mu.Lock()
			results = append(results, r)
			n := len(results)
			mu.Unlock()

			if !quiet && n%100 == 0 {
				fmt.Fprintf(os.Stderr, "\r  %d requests…", n)
			}
		}(ch, reqStart)
	}

	wg.Wait()
	elapsed := time.Since(start)

	if !quiet {
		fmt.Fprintln(os.Stderr)
	}

	return buildStats(results, elapsed)
}

// buildStats computes Stats from raw result records and total elapsed time.
func buildStats(results []result, elapsed time.Duration) *Stats {
	s := &Stats{
		Duration:    duration(elapsed),
		StatusCodes: make(map[int]int),
		Total:       len(results),
	}

	if len(results) == 0 {
		return s
	}

	latencies := make([]time.Duration, 0, len(results))
	var sum time.Duration

	for _, r := range results {
		if r.err != nil {
			s.Errors++
			continue
		}
		if r.statusCode >= 400 {
			s.Failures++
		} else {
			s.Successes++
		}
		s.StatusCodes[r.statusCode]++
		latencies = append(latencies, r.latency)
		sum += r.latency
	}

	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		s.LatencyMin = duration(latencies[0])
		s.LatencyMax = duration(latencies[len(latencies)-1])
		s.LatencyMean = duration(sum / time.Duration(len(latencies)))
		s.LatencyP50 = duration(percentile(latencies, 50))
		s.LatencyP95 = duration(percentile(latencies, 95))
		s.LatencyP99 = duration(percentile(latencies, 99))
	}

	if elapsed > 0 {
		s.RPS = float64(s.Total) / elapsed.Seconds()
	}

	return s
}

// percentile returns the p-th percentile value from a pre-sorted slice.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(len(sorted))*p/100)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// printStats writes a human-readable benchmark summary to stdout.
func printStats(s *Stats) {
	line := "────────────────────────────────────────────────"
	fmt.Printf("\n%s\n", line)
	fmt.Printf("  relay-bench results\n")
	fmt.Printf("%s\n", line)
	fmt.Printf("  URL          %s %s\n", s.Method, s.URL)
	fmt.Printf("  Concurrency  %d\n", s.Concurrency)
	fmt.Printf("  Duration     %s\n", time.Duration(s.Duration).Round(time.Millisecond))
	fmt.Printf("  Requests     %d total  (✓ %d  ✗ %d  err %d)\n", s.Total, s.Successes, s.Failures, s.Errors)
	fmt.Printf("  Throughput   %.2f req/s\n", s.RPS)
	fmt.Printf("\n  Latency (across successful responses):\n")
	fmt.Printf("    min   %v\n", time.Duration(s.LatencyMin).Round(time.Microsecond))
	fmt.Printf("    mean  %v\n", time.Duration(s.LatencyMean).Round(time.Microsecond))
	fmt.Printf("    p50   %v\n", time.Duration(s.LatencyP50).Round(time.Microsecond))
	fmt.Printf("    p95   %v\n", time.Duration(s.LatencyP95).Round(time.Microsecond))
	fmt.Printf("    p99   %v\n", time.Duration(s.LatencyP99).Round(time.Microsecond))
	fmt.Printf("    max   %v\n", time.Duration(s.LatencyMax).Round(time.Microsecond))

	if len(s.StatusCodes) > 0 {
		fmt.Printf("\n  Status codes:\n")
		codes := sortedIntKeys(s.StatusCodes)
		for _, c := range codes {
			fmt.Printf("    HTTP %d  ×%d\n", c, s.StatusCodes[c])
		}
	}

	if len(s.CBTransitions) > 0 {
		fmt.Printf("\n  Circuit breaker transitions:\n")
		for _, t := range s.CBTransitions {
			fmt.Printf("    %s\n", t)
		}
	}

	fmt.Printf("%s\n\n", line)
}

func sortedIntKeys(m map[int]int) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
