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

func TestPutSizedBuffer_Nil(t *testing.T) {
	PutSizedBuffer(nil) // should not panic
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


