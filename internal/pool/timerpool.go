package pool

import (
	"sync"
	"time"
)

var timerPool = &sync.Pool{
	New: func() any {
		return time.NewTimer(0)
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
