package relay

import (
	"net/http"
	"testing"
	"time"

	"github.com/jhonsferg/relay/internal/pool"
	"github.com/jhonsferg/relay/testutil"
)

func TestTiming_TotalIsPositiveAfterExecute(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: "timing-body"})

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
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

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
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

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())
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
	col := &pool.TimingCollector{}
	total := 42 * time.Millisecond

	timing := buildTiming(col, total)
	if timing.Total != total {
		t.Errorf("expected Total=%v, got %v", total, timing.Total)
	}
}

func TestBuildTiming_DNSLookupComputed(t *testing.T) {
	now := time.Now()
	col := &pool.TimingCollector{
		DNSStart: now,
		DNSDone:  now.Add(5 * time.Millisecond),
	}

	timing := buildTiming(col, 100*time.Millisecond)
	if timing.DNSLookup != 5*time.Millisecond {
		t.Errorf("expected DNSLookup=5ms, got %v", timing.DNSLookup)
	}
}

func TestBuildTiming_TCPConnectComputed(t *testing.T) {
	now := time.Now()
	col := &pool.TimingCollector{
		ConnStart: now,
		ConnDone:  now.Add(10 * time.Millisecond),
	}

	timing := buildTiming(col, 100*time.Millisecond)
	if timing.TCPConnect != 10*time.Millisecond {
		t.Errorf("expected TCPConnect=10ms, got %v", timing.TCPConnect)
	}
}

func TestBuildTiming_ZeroWhenTimestampsMissing(t *testing.T) {
	col := &pool.TimingCollector{} // all zero times

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

	c := New(WithDisableRetry(), WithDisableCircuitBreaker())

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
