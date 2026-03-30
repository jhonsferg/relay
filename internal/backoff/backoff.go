// Package backoff provides exponential backoff with full jitter for retry logic.
package backoff

import (
	"math"
	"math/rand"
	"time"
)

// Config holds parameters for exponential backoff with optional jitter.
type Config struct {
	// InitialInterval is the base sleep duration before the first retry.
	InitialInterval time.Duration

	// MaxInterval is the upper bound on the computed sleep duration.
	MaxInterval time.Duration

	// Multiplier is applied to the interval on each successive attempt.
	// A value of 2.0 doubles the interval each time.
	Multiplier float64

	// RandomFactor is the fraction of the interval added as jitter.
	// 0.0 = no jitter, 1.0 = up to ±100% jitter (full jitter).
	RandomFactor float64
}

// Next returns the sleep duration for the given attempt number (0-indexed).
// It computes: min(MaxInterval, InitialInterval * Multiplier^attempt) * (1 ± RandomFactor).
func (c Config) Next(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	base := float64(c.InitialInterval) * math.Pow(c.Multiplier, float64(attempt))
	if base > float64(c.MaxInterval) {
		base = float64(c.MaxInterval)
	}

	if c.RandomFactor > 0 {
		// Full jitter: random value in [0, base * RandomFactor]
		jitter := rand.Float64() * c.RandomFactor * base
		base = base - (c.RandomFactor*base)/2 + jitter
	}

	if base < 0 {
		base = 0
	}

	return time.Duration(base)
}

// DefaultConfig returns a backoff configuration suitable for most use cases.
// 100ms initial, 30s max, 2x multiplier, 50% jitter.
func DefaultConfig() Config {
	return Config{
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     30 * time.Second,
		Multiplier:      2.0,
		RandomFactor:    0.5,
	}
}
