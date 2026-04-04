package backoff

import (
	"testing"
	"time"
)

func TestNext_ZeroAttempt(t *testing.T) {
	c := Config{
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     10 * time.Second,
		Multiplier:      2.0,
		RandomFactor:    0,
	}
	d := c.Next(0)
	if d != 100*time.Millisecond {
		t.Errorf("attempt 0 got %v, want 100ms", d)
	}
}

func TestNext_NegativeAttemptTreatedAsZero(t *testing.T) {
	c := Config{
		InitialInterval: 200 * time.Millisecond,
		MaxInterval:     10 * time.Second,
		Multiplier:      2.0,
		RandomFactor:    0,
	}
	if c.Next(-5) != c.Next(0) {
		t.Error("negative attempt should equal attempt 0")
	}
}

func TestNext_ExponentialGrowth(t *testing.T) {
	c := Config{
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     10 * time.Second,
		Multiplier:      2.0,
		RandomFactor:    0,
	}
	prev := c.Next(0)
	for i := 1; i <= 5; i++ {
		next := c.Next(i)
		if next <= prev {
			t.Errorf("attempt %d (%v) should be greater than attempt %d (%v)", i, next, i-1, prev)
		}
		prev = next
	}
}

func TestNext_CappedAtMaxInterval(t *testing.T) {
	c := Config{
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     500 * time.Millisecond,
		Multiplier:      10.0,
		RandomFactor:    0,
	}
	// After enough attempts, should be capped at MaxInterval
	d := c.Next(10)
	if d > c.MaxInterval {
		t.Errorf("got %v, expected <= %v", d, c.MaxInterval)
	}
}

func TestNext_WithJitter(t *testing.T) {
	c := Config{
		InitialInterval: 1 * time.Second,
		MaxInterval:     10 * time.Second,
		Multiplier:      2.0,
		RandomFactor:    0.5,
	}
	// Collect several values - should not all be identical due to jitter
	seen := map[time.Duration]bool{}
	for i := 0; i < 20; i++ {
		seen[c.Next(1)] = true
	}
	if len(seen) < 2 {
		t.Error("jitter expected to produce varied durations")
	}
}

func TestNext_NonNegative(t *testing.T) {
	c := Config{
		InitialInterval: 1 * time.Millisecond,
		MaxInterval:     10 * time.Millisecond,
		Multiplier:      2.0,
		RandomFactor:    1.0, // max jitter
	}
	for i := 0; i < 50; i++ {
		d := c.Next(i)
		if d < 0 {
			t.Errorf("attempt %d returned negative duration %v", i, d)
		}
	}
}

func TestNext_ZeroMultiplier(t *testing.T) {
	c := Config{
		InitialInterval: 200 * time.Millisecond,
		MaxInterval:     10 * time.Second,
		Multiplier:      0,
		RandomFactor:    0,
	}
	// 200ms * 0^n = 0 for n>0; attempt 0 = 200ms * 0^0 = 200ms * 1 = 200ms
	d := c.Next(0)
	if d != 200*time.Millisecond {
		t.Errorf("got %v, want 200ms", d)
	}
}

func TestNext_LargeAttemptStaysAtMax(t *testing.T) {
	c := Config{
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     5 * time.Second,
		Multiplier:      2.0,
		RandomFactor:    0,
	}
	d := c.Next(100)
	if d > c.MaxInterval {
		t.Errorf("large attempt produced %v, want <= %v", d, c.MaxInterval)
	}
}
