package pool

import (
	"sync"
	"time"
)

var timerPool = &sync.Pool{
	New: func() any {
		// time.NewTimer(0) fires almost immediately. Stop+drain it before
		// handing it out so GetTimer's Reset(d) below always operates on a
		// stopped, drained timer, per time.Timer.Reset's documented contract
		// - otherwise a pool-miss could race the near-instant fire against
		// Reset, leaving a stale wakeup in t.C that a later <-t.C consumes
		// immediately instead of waiting the intended duration.
		t := time.NewTimer(0)
		if !t.Stop() {
			<-t.C
		}
		return t
	},
}

// GetTimer returns a pooled *time.Timer configured for the given duration.
// The timer is reset to d and ready for use. Must be returned via PutTimer
// when done.
func GetTimer(d time.Duration) *time.Timer {
	t := timerPool.Get().(*time.Timer)
	t.Reset(d)
	return t
}

// PutTimer returns a timer to the pool. Must be called when done with the timer
// to allow reuse. Ensures timer is stopped and the channel is drained.
func PutTimer(t *time.Timer) {
	if t == nil {
		return
	}
	// Stop the timer and drain any pending signal to reset it for reuse
	if !t.Stop() {
		// Timer already fired - drain the channel
		select {
		case <-t.C:
		default:
		}
	}
	timerPool.Put(t)
}
