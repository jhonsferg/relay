package relay

import (
	"context"
	"net/http"
	"net/http/httptrace"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

func TestTiming_TotalIsPositiveAfterExecute(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "timing-body"})

	c := New(WithTiming(), WithDisableRetry(), WithDisableCircuitBreaker())
	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if resp.Timing.Total <= 0 {
		t.Errorf("expected Timing.Total > 0, got %v", resp.Timing.Total)
	}
}

func TestTiming_TotalReflectsActualElapsed(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	delay := 50 * time.Millisecond
	srv.Enqueue(testutil.MockResponse{
		Status: http.StatusOK,
		Body:   "slow",
		Delay:  delay,
	})

	c := New(WithTiming(), WithDisableRetry(), WithDisableCircuitBreaker())
	start := time.Now()
	resp, err := c.Execute(c.Get(srv.URL() + "/slow"))
	wallClock := time.Since(start)

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if resp.Timing.Total < delay {
		t.Errorf("Timing.Total (%v) should be >= server delay (%v)", resp.Timing.Total, delay)
	}
	// Timing should be in the same ballpark as wall-clock (within 2x).
	if resp.Timing.Total > 2*wallClock {
		t.Errorf("Timing.Total (%v) is much larger than wall-clock (%v)", resp.Timing.Total, wallClock)
	}
}

func TestTiming_NonNegativeBreakdown(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "breakdown"})

	c := New(WithTiming(), WithDisableRetry(), WithDisableCircuitBreaker())
	resp, err := c.Execute(c.Get(srv.URL() + "/"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	timing := resp.Timing

	if timing.DNSLookup < 0 {
		t.Errorf("DNSLookup should be >= 0, got %v", timing.DNSLookup)
	}
	if timing.TCPConnect < 0 {
		t.Errorf("TCPConnect should be >= 0, got %v", timing.TCPConnect)
	}
	if timing.TLSHandshake < 0 {
		t.Errorf("TLSHandshake should be >= 0, got %v", timing.TLSHandshake)
	}
	if timing.TimeToFirstByte < 0 {
		t.Errorf("TimeToFirstByte should be >= 0, got %v", timing.TimeToFirstByte)
	}
	if timing.ContentTransfer < 0 {
		t.Errorf("ContentTransfer should be >= 0, got %v", timing.ContentTransfer)
	}
}

func TestBuildTiming_TotalSet(t *testing.T) {
	col := &timingCollector{}
	total := 42 * time.Millisecond

	timing := buildTiming(col, total)
	if timing.Total != total {
		t.Errorf("expected Total=%v, got %v", total, timing.Total)
	}
}

func TestBuildTiming_DNSLookupComputed(t *testing.T) {
	now := time.Now()
	col := &timingCollector{}
	col.dnsStart.Store(now.UnixNano())
	col.dnsDone.Store(now.Add(5 * time.Millisecond).UnixNano())

	timing := buildTiming(col, 100*time.Millisecond)
	if timing.DNSLookup != 5*time.Millisecond {
		t.Errorf("expected DNSLookup=5ms, got %v", timing.DNSLookup)
	}
}

func TestBuildTiming_TCPConnectComputed(t *testing.T) {
	now := time.Now()
	col := &timingCollector{}
	col.connStart.Store(now.UnixNano())
	col.connDone.Store(now.Add(10 * time.Millisecond).UnixNano())

	timing := buildTiming(col, 100*time.Millisecond)
	if timing.TCPConnect != 10*time.Millisecond {
		t.Errorf("expected TCPConnect=10ms, got %v", timing.TCPConnect)
	}
}

func TestBuildTiming_ZeroWhenTimestampsMissing(t *testing.T) {
	col := &timingCollector{} // all zero (never stored)

	timing := buildTiming(col, 50*time.Millisecond)
	if timing.DNSLookup != 0 {
		t.Errorf("DNSLookup should be 0 when timestamps missing, got %v", timing.DNSLookup)
	}
	if timing.TCPConnect != 0 {
		t.Errorf("TCPConnect should be 0 when timestamps missing, got %v", timing.TCPConnect)
	}
	if timing.TLSHandshake != 0 {
		t.Errorf("TLSHandshake should be 0 when timestamps missing, got %v", timing.TLSHandshake)
	}
}

func TestTiming_MultipleRequests(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	for i := 0; i < 3; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "multi"})
	}

	c := New(WithTiming(), WithDisableRetry(), WithDisableCircuitBreaker())

	for i := 0; i < 3; i++ {
		resp, err := c.Execute(c.Get(srv.URL() + "/"))
		if err != nil {
			t.Fatalf("Execute %d: %v", i, err)
		}
		if resp.Timing.Total <= 0 {
			t.Errorf("request %d: Timing.Total should be > 0, got %v", i, resp.Timing.Total)
		}
	}
}

// TestInjectTraceContext_NoCrossRequestCorruption guards against a data
// corruption bug: an earlier version pooled a single
// (*timingCollector, *httptrace.ClientTrace) pair via sync.Pool and reused
// it across unrelated requests. Since the trace's closures close over the
// collector by reference, a callback that fires "late" (e.g. from a
// net/http dialParallel "loser" goroutine racing IPv4/IPv6, which can fire
// after the request that raced it already completed) wrote into whichever
// *different* request had since been leased the same reused object -
// silently corrupting its Response.Timing with a foreign timestamp.
//
// This reproduces it deterministically without needing a real network dial
// race: lease A, return A's collector, lease B, fire A's now-stale
// callback directly, and confirm B's field was NOT written by it.
func TestInjectTraceContext_NoCrossRequestCorruption(t *testing.T) {
	// Lease 1: simulate "request A".
	ctxA, colA := injectTraceContext(context.Background())
	traceA := httptrace.ContextClientTrace(ctxA)
	if traceA == nil {
		t.Fatal("no trace attached to ctxA")
	}

	// Request A finishes and returns its collector - simulating a "loser"
	// dial goroutine still in flight, not yet having fired its callback.
	putTimingCollector(colA)

	// Lease 2: simulate "request B" starting immediately after.
	_, colB := injectTraceContext(context.Background())

	// request A's late "loser" dial goroutine now fires ConnectDone.
	traceA.ConnectDone("tcp", "10.0.0.1:443", nil)

	// colA and colB must be independent instances - A's stale callback must
	// only ever be able to write into colA, never colB.
	if colA == colB {
		t.Fatal("collectors are pooled/shared across requests - this test cannot demonstrate independence")
	}
	if colB.connDone.Load() != 0 {
		t.Errorf("request B's connDone = %d, want 0 (untouched) - request A's stale callback corrupted it", colB.connDone.Load())
	}
	if colA.connDone.Load() == 0 {
		t.Error("request A's own connDone was not set by its own callback - test setup is broken")
	}
}
