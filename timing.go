package relay

import (
	"context"
	"crypto/tls"
	"net/http/httptrace"
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

// timingCollector is used internally to accumulate timing checkpoints.
type timingCollector struct {
	dnsStart     time.Time
	dnsDone      time.Time
	connStart    time.Time
	connDone     time.Time
	tlsStart     time.Time
	tlsDone      time.Time
	firstByte    time.Time
	requestStart time.Time
}

// injectTraceContext returns a new context with an httptrace.ClientTrace
// attached. The collector is populated as the request progresses.
func injectTraceContext(ctx context.Context, col *timingCollector) context.Context {
	col.requestStart = time.Now()

	trace := &httptrace.ClientTrace{
		DNSStart: func(_ httptrace.DNSStartInfo) {
			col.dnsStart = time.Now()
		},
		DNSDone: func(_ httptrace.DNSDoneInfo) {
			col.dnsDone = time.Now()
		},
		ConnectStart: func(_, _ string) {
			col.connStart = time.Now()
		},
		ConnectDone: func(_, _ string, _ error) {
			col.connDone = time.Now()
		},
		TLSHandshakeStart: func() {
			col.tlsStart = time.Now()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			col.tlsDone = time.Now()
		},
		GotFirstResponseByte: func() {
			col.firstByte = time.Now()
		},
	}
	return httptrace.WithClientTrace(ctx, trace)
}

// buildTiming computes the RequestTiming from collected checkpoints.
// total is the wall-clock duration of the entire Execute call.
func buildTiming(col *timingCollector, total time.Duration) RequestTiming {
	t := RequestTiming{Total: total}

	if !col.dnsStart.IsZero() && !col.dnsDone.IsZero() {
		t.DNSLookup = col.dnsDone.Sub(col.dnsStart)
	}
	if !col.connStart.IsZero() && !col.connDone.IsZero() {
		t.TCPConnect = col.connDone.Sub(col.connStart)
	}
	if !col.tlsStart.IsZero() && !col.tlsDone.IsZero() {
		t.TLSHandshake = col.tlsDone.Sub(col.tlsStart)
	}
	if !col.requestStart.IsZero() && !col.firstByte.IsZero() {
		t.TimeToFirstByte = col.firstByte.Sub(col.requestStart)
	}
	if !col.firstByte.IsZero() {
		t.ContentTransfer = total - col.firstByte.Sub(col.requestStart)
		if t.ContentTransfer < 0 {
			t.ContentTransfer = 0
		}
	}

	return t
}
