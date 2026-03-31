package relay

import (
	"context"
	"crypto/tls"
	"net/http/httptrace"
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

// injectTraceContext attaches an httptrace.ClientTrace to ctx and returns a
// per-request timingCollector. All writes go through atomic stores so the
// transport's background goroutines (dialParallel) can write concurrently
// without racing against the buildTiming read that follows Do().
func injectTraceContext(ctx context.Context) (context.Context, *timingCollector) {
	col := &timingCollector{}
	col.requestStart.Store(nowNano())

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
	return httptrace.WithClientTrace(ctx, trace), col
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
