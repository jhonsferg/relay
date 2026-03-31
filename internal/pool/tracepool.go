package pool

import (
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"time"
)

// TimingCollector accumulates timing checkpoints during an HTTP request.
type TimingCollector struct {
	DNSStart     time.Time
	DNSDone      time.Time
	ConnStart    time.Time
	ConnDone     time.Time
	TLSStart     time.Time
	TLSDone      time.Time
	FirstByte    time.Time
	RequestStart time.Time
}

// Reset clears all timing values for reuse.
func (tc *TimingCollector) Reset() {
	tc.DNSStart = time.Time{}
	tc.DNSDone = time.Time{}
	tc.ConnStart = time.Time{}
	tc.ConnDone = time.Time{}
	tc.TLSStart = time.Time{}
	tc.TLSDone = time.Time{}
	tc.FirstByte = time.Time{}
	tc.RequestStart = time.Time{}
}

// tracerEntry holds both collector and trace for pooled reuse.
type tracerEntry struct {
	collector *TimingCollector
	trace     *httptrace.ClientTrace
}

var tracerPool = &sync.Pool{
	New: func() any {
		col := &TimingCollector{}

		// Create trace with closures capturing the collector pointer.
		// Safe because entry is not reused until explicitly returned to pool.
		trace := &httptrace.ClientTrace{
			DNSStart: func(_ httptrace.DNSStartInfo) {
				col.DNSStart = time.Now()
			},
			DNSDone: func(_ httptrace.DNSDoneInfo) {
				col.DNSDone = time.Now()
			},
			ConnectStart: func(_, _ string) {
				col.ConnStart = time.Now()
			},
			ConnectDone: func(_, _ string, _ error) {
				col.ConnDone = time.Now()
			},
			TLSHandshakeStart: func() {
				col.TLSStart = time.Now()
			},
			TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
				col.TLSDone = time.Now()
			},
			GotFirstResponseByte: func() {
				col.FirstByte = time.Now()
			},
		}

		return &tracerEntry{
			collector: col,
			trace:     trace,
		}
	},
}

// GetTracer returns a pooled TimingCollector and ClientTrace.
// The collector is populated as the request progresses.
// Must be returned via PutTracer when done.
func GetTracer() (*TimingCollector, *httptrace.ClientTrace) {
	entry := tracerPool.Get().(*tracerEntry)
	entry.collector.RequestStart = time.Now()
	return entry.collector, entry.trace
}

// PutTracer returns a tracer entry to the pool.
// Must be called after timing is finalized to reset for reuse.
func PutTracer(col *TimingCollector) {
	col.Reset()
	entry := &tracerEntry{collector: col}
	tracerPool.Put(entry)
}
