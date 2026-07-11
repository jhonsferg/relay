package relay

import (
	"context"
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"sync/atomic"
	"time"
)

// RequestTiming holds a detailed breakdown of the time spent in each phase of
// an HTTP request.
type RequestTiming struct {
	// DNSLookup is the time spent resolving the hostname.
	DNSLookup time.Duration

	// TCPConnect is the time to establish the TCP connection.
	TCPConnect time.Duration

	// TLSHandshake is the time to complete the TLS handshake (0 for plain HTTP).
	TLSHandshake time.Duration

	// TimeToFirstByte (TTFB) is the time from sending the request until the
	// first byte of the response was received.
	TimeToFirstByte time.Duration

	// ContentTransfer is the time from first byte to the full body being read.
	ContentTransfer time.Duration

	// Total is the end-to-end wall-clock duration of the request.
	Total time.Duration
}

// timingCollector accumulates timing checkpoints during an HTTP request.
//
// All checkpoint fields use sync/atomic.Int64 (Unix nanoseconds) instead of
// time.Time because net/http's dialParallel fires ConnectStart/ConnectDone
// callbacks from background goroutines that can outlive httpClient.Do(). Plain
// time.Time fields would race; atomic stores/loads are safe by definition.
type timingCollector struct {
	dnsStart     atomic.Int64
	dnsDone      atomic.Int64
	connStart    atomic.Int64
	connDone     atomic.Int64
	tlsStart     atomic.Int64
	tlsDone      atomic.Int64
	firstByte    atomic.Int64
	requestStart atomic.Int64
	entry        atomic.Pointer[timingEntry]
}

// reset clears all checkpoint fields so the collector can be reused.
func (tc *timingCollector) reset() {
	tc.dnsStart.Store(0)
	tc.dnsDone.Store(0)
	tc.connStart.Store(0)
	tc.connDone.Store(0)
	tc.tlsStart.Store(0)
	tc.tlsDone.Store(0)
	tc.firstByte.Store(0)
	tc.requestStart.Store(0)
}

// nowNano returns the current time as Unix nanoseconds.
func nowNano() int64 { return time.Now().UnixNano() }

// toTime converts a stored nanosecond timestamp back to time.Time.
// Returns zero time when v == 0 (field was never set).
func toTime(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, v)
}

// timingEntry pairs a collector and its pre-allocated trace for pool reuse.
// The trace closures capture the collector pointer so they always write to
// the same instance. atomic.Int64 fields prevent races even if dialParallel
// callbacks fire after Do() returns.
type timingEntry struct {
	col   *timingCollector
	trace *httptrace.ClientTrace
}

var timingPool sync.Pool

func init() {
	timingPool.New = func() any {
		col := &timingCollector{}
		trace := &httptrace.ClientTrace{
			DNSStart: func(_ httptrace.DNSStartInfo) {
				col.dnsStart.Store(nowNano())
			},
			DNSDone: func(_ httptrace.DNSDoneInfo) {
				col.dnsDone.Store(nowNano())
			},
			ConnectStart: func(_, _ string) {
				col.connStart.Store(nowNano())
			},
			ConnectDone: func(_, _ string, _ error) {
				col.connDone.Store(nowNano())
			},
			TLSHandshakeStart: func() {
				col.tlsStart.Store(nowNano())
			},
			TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
				col.tlsDone.Store(nowNano())
			},
			GotFirstResponseByte: func() {
				col.firstByte.Store(nowNano())
			},
		}
		return &timingEntry{col: col, trace: trace}
	}
}

// injectTraceContext attaches an httptrace.ClientTrace to ctx and returns a
// per-request timingCollector from the pool. All writes go through atomic
// stores so the transport's background goroutines (dialParallel) can write
// concurrently without racing against the buildTiming read that follows Do().
//
// The caller must call putTimingCollector when done to return the entry
// to the pool for reuse.
func injectTraceContext(ctx context.Context) (context.Context, *timingCollector) {
	entry := timingPool.Get().(*timingEntry)
	entry.col.reset()
	entry.col.entry.Store(entry)
	entry.col.requestStart.Store(nowNano())
	return httptrace.WithClientTrace(ctx, entry.trace), entry.col
}

// putTimingCollector returns a timing collector to the pool for reuse.
// Must be called after buildTiming has consumed the timing values.
func putTimingCollector(col *timingCollector) {
	if col != nil && col.entry.Load() != nil {
		timingPool.Put(col.entry.Load())
		col.entry.Store(nil)
	}
}

// buildTiming computes RequestTiming from atomic-loaded checkpoints.
// total is the wall-clock duration of the entire Execute call.
func buildTiming(col *timingCollector, total time.Duration) RequestTiming {
	t := RequestTiming{Total: total}

	dnsStart := toTime(col.dnsStart.Load())
	dnsDone := toTime(col.dnsDone.Load())
	connStart := toTime(col.connStart.Load())
	connDone := toTime(col.connDone.Load())
	tlsStart := toTime(col.tlsStart.Load())
	tlsDone := toTime(col.tlsDone.Load())
	requestStart := toTime(col.requestStart.Load())
	firstByte := toTime(col.firstByte.Load())

	if !dnsStart.IsZero() && !dnsDone.IsZero() {
		t.DNSLookup = dnsDone.Sub(dnsStart)
	}
	if !connStart.IsZero() && !connDone.IsZero() {
		t.TCPConnect = connDone.Sub(connStart)
	}
	if !tlsStart.IsZero() && !tlsDone.IsZero() {
		t.TLSHandshake = tlsDone.Sub(tlsStart)
	}
	if !requestStart.IsZero() && !firstByte.IsZero() {
		t.TimeToFirstByte = firstByte.Sub(requestStart)
	}
	if !firstByte.IsZero() {
		t.ContentTransfer = total - firstByte.Sub(requestStart)
		if t.ContentTransfer < 0 {
			t.ContentTransfer = 0
		}
	}

	return t
}
