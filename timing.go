package relay

import (
	"context"
	"net/http/httptrace"
	"time"

	"github.com/jhonsferg/relay/internal/pool"
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

// injectTraceContext returns a new context with an httptrace.ClientTrace attached
// and a pooled TimingCollector. The collector is populated as the request progresses.
func injectTraceContext(ctx context.Context) (context.Context, *pool.TimingCollector) {
	col, trace := pool.GetTracer()
	return httptrace.WithClientTrace(ctx, trace), col
}

// buildTiming computes the RequestTiming from collected checkpoints.
// total is the wall-clock duration of the entire Execute call.
func buildTiming(col *pool.TimingCollector, total time.Duration) RequestTiming {
	t := RequestTiming{Total: total}

	if !col.DNSStart.IsZero() && !col.DNSDone.IsZero() {
		t.DNSLookup = col.DNSDone.Sub(col.DNSStart)
	}
	if !col.ConnStart.IsZero() && !col.ConnDone.IsZero() {
		t.TCPConnect = col.ConnDone.Sub(col.ConnStart)
	}
	if !col.TLSStart.IsZero() && !col.TLSDone.IsZero() {
		t.TLSHandshake = col.TLSDone.Sub(col.TLSStart)
	}
	if !col.RequestStart.IsZero() && !col.FirstByte.IsZero() {
		t.TimeToFirstByte = col.FirstByte.Sub(col.RequestStart)
	}
	if !col.FirstByte.IsZero() {
		t.ContentTransfer = total - col.FirstByte.Sub(col.RequestStart)
		if t.ContentTransfer < 0 {
			t.ContentTransfer = 0
		}
	}

	return t
}
