package pool

import (
	"bytes"
	"fmt"
	"sync"
	"testing"
	"time"
)

// ----- readerpool -----

func TestGetBytesReader(t *testing.T) {
	data := []byte("hello world")
	r := GetBytesReader(data)
	if r == nil {
		t.Fatal("expected non-nil reader")
	}
	buf := make([]byte, len(data))
	n, _ := r.Read(buf)
	if string(buf[:n]) != "hello world" {
		t.Errorf("read %q, want %q", buf[:n], data)
	}
	PutBytesReader(r)
}

func TestPutBytesReader_Nil(t *testing.T) {
	PutBytesReader(nil) // should not panic
}

func TestGetBytesReader_Reuse(t *testing.T) {
	r1 := GetBytesReader([]byte("first"))
	PutBytesReader(r1)
	r2 := GetBytesReader([]byte("second"))
	buf := make([]byte, 6)
	n, _ := r2.Read(buf)
	if string(buf[:n]) != "second" {
		t.Errorf("reused reader got %q, want %q", buf[:n], "second")
	}
	PutBytesReader(r2)
}

func TestGetBytesReader_Empty(t *testing.T) {
	r := GetBytesReader(nil)
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	if buf.Len() != 0 {
		t.Errorf("expected empty read, got %d bytes", buf.Len())
	}
	PutBytesReader(r)
}

// ----- timerpool -----

func TestGetTimer(t *testing.T) {
	timer := GetTimer(50 * time.Millisecond)
	if timer == nil {
		t.Fatal("expected non-nil timer")
	}
	select {
	case <-timer.C:
		// fired as expected
	case <-time.After(200 * time.Millisecond):
		t.Error("timer did not fire within 200ms")
	}
	PutTimer(timer)
}

func TestPutTimer_Nil(t *testing.T) {
	PutTimer(nil) // should not panic
}

func TestPutTimer_AlreadyFired(t *testing.T) {
	timer := GetTimer(1 * time.Millisecond)
	time.Sleep(10 * time.Millisecond) // let it fire
	PutTimer(timer)                   // should not panic even if already fired
}

func TestGetTimer_Reuse(t *testing.T) {
	t1 := GetTimer(10 * time.Millisecond)
	PutTimer(t1)
	t2 := GetTimer(10 * time.Millisecond)
	if t2 == nil {
		t.Error("expected non-nil reused timer")
	}
	PutTimer(t2)
}

// TestTimerPoolNew_ChannelDrained guards against a regression where a
// pool-miss handed out a timer whose channel could already hold a pending
// value: timerPool.New wraps time.NewTimer(0), which fires almost
// immediately, but previously did not Stop+drain it before returning it.
// time.Timer.Reset's documented contract requires the timer be stopped and
// drained first; violating it on a cold-miss meant GetTimer's Reset(d) could
// leave the near-instant original fire sitting undrained in the channel, so
// a later <-timer.C in retry/rate-limit code returned immediately instead of
// waiting the requested duration.
func TestTimerPoolNew_ChannelDrained(t *testing.T) {
	for i := 0; i < 20; i++ {
		raw := timerPool.New()
		timer, ok := raw.(*time.Timer)
		if !ok {
			t.Fatalf("New() returned %T, want *time.Timer", raw)
		}
		time.Sleep(time.Millisecond) // let an undrained 0-duration fire land, if present
		select {
		case <-timer.C:
			t.Fatalf("iteration %d: timer channel had a pending value immediately after New() - not drained", i)
		default:
		}
	}
}

// TestGetTimer_ConcurrentColdMisses forces many concurrent pool-misses (by
// requesting far more timers than have ever been returned to the pool) and
// verifies none of them fire meaningfully earlier than the requested
// duration.
func TestGetTimer_ConcurrentColdMisses(t *testing.T) {
	const (
		n    = 50
		want = 30 * time.Millisecond
	)
	var wg sync.WaitGroup
	errs := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start := time.Now()
			timer := GetTimer(want)
			defer PutTimer(timer)
			<-timer.C
			if elapsed := time.Since(start); elapsed < want/2 {
				errs <- fmt.Sprintf("goroutine %d: timer fired after %v, want ~%v", i, elapsed, want)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for msg := range errs {
		t.Error(msg)
	}
}
