package pool

import (
	"bytes"
	"testing"
	"time"
)

// ----- bytespool -----

func TestGetSizedBuffer_Small(t *testing.T) {
	b := GetSizedBuffer(1024)
	if b == nil {
		t.Fatal("expected non-nil buffer")
	}
	if cap(*b) != smallBufferSize {
		t.Errorf("cap = %d, want %d", cap(*b), smallBufferSize)
	}
	PutSizedBuffer(b)
}

func TestGetSizedBuffer_Medium(t *testing.T) {
	b := GetSizedBuffer(smallBufferSize + 1)
	if cap(*b) != mediumBufferSize {
		t.Errorf("cap = %d, want %d", cap(*b), mediumBufferSize)
	}
	PutSizedBuffer(b)
}

func TestGetSizedBuffer_Large(t *testing.T) {
	b := GetSizedBuffer(mediumBufferSize + 1)
	if cap(*b) != largeBufferSize {
		t.Errorf("cap = %d, want %d", cap(*b), largeBufferSize)
	}
	PutSizedBuffer(b)
}

func TestGetSizedBuffer_Huge(t *testing.T) {
	b := GetSizedBuffer(largeBufferSize + 1)
	if cap(*b) != hugeBufferSize {
		t.Errorf("cap = %d, want %d", cap(*b), hugeBufferSize)
	}
	PutSizedBuffer(b)
}

func TestGetBuffer_Deprecated(t *testing.T) {
	b := GetBuffer()
	if b == nil {
		t.Fatal("expected non-nil buffer from GetBuffer")
	}
	if cap(*b) != mediumBufferSize {
		t.Errorf("cap = %d, want %d", cap(*b), mediumBufferSize)
	}
	PutBuffer(b)
}

func TestPutSizedBuffer_Nil(t *testing.T) {
	PutSizedBuffer(nil) // should not panic
}

func TestPutBuffer_Nil(t *testing.T) {
	PutBuffer(nil) // should not panic
}

func TestPutSizedBuffer_UnknownSize(t *testing.T) {
	// A buffer with a non-standard cap should be silently dropped (no pool match).
	b := make([]byte, 12345)
	PutSizedBuffer(&b) // should not panic
}

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

// ----- tracepool -----

func TestGetTracer(t *testing.T) {
	col, trace := GetTracer()
	if col == nil {
		t.Fatal("expected non-nil TimingCollector")
	}
	if trace == nil {
		t.Fatal("expected non-nil ClientTrace")
	}
	if col.RequestStart.IsZero() {
		t.Error("RequestStart should be set by GetTracer")
	}
	PutTracer(col)
}

func TestPutTracer_ResetsFields(t *testing.T) {
	col, _ := GetTracer()
	// Simulate some timing events
	col.DNSStart = time.Now()
	col.FirstByte = time.Now()
	PutTracer(col)

	// Get again - should be reset
	col2, _ := GetTracer()
	if !col2.DNSDone.IsZero() {
		t.Error("DNSDone should be zero after reset")
	}
	PutTracer(col2)
}

func TestGetTracer_Reuse(t *testing.T) {
	col1, _ := GetTracer()
	PutTracer(col1)
	col2, trace2 := GetTracer()
	if col2 == nil || trace2 == nil {
		t.Error("reuse should return non-nil")
	}
	PutTracer(col2)
}

func TestTimingCollector_Reset(t *testing.T) {
	tc := &TimingCollector{
		DNSStart:  time.Now(),
		FirstByte: time.Now(),
	}
	tc.Reset()
	if !tc.DNSStart.IsZero() || !tc.FirstByte.IsZero() {
		t.Error("Reset() should zero all time fields")
	}
}
