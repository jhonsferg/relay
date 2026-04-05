package relay

import (
	"sort"
	"sync"
	"time"
)

// AdaptiveTimeoutConfig defines the parameters for adaptive timeout adjustment
// based on observed response latencies.
type AdaptiveTimeoutConfig struct {
	// Percentile is the latency percentile to use as the base (e.g. 0.95 for p95).
	// Default: 0.95.
	Percentile float64
	// Multiplier scales the percentile latency to get the timeout (e.g. 2.0 = 2x p95).
	// Default: 2.0.
	Multiplier float64
	// WindowSize is the number of recent observations to keep.
	// Default: 100.
	WindowSize int
	// MinTimeout is the minimum timeout regardless of observed latency.
	// Default: 100ms.
	MinTimeout time.Duration
	// MaxTimeout is the maximum timeout cap.
	// Default: 30s.
	MaxTimeout time.Duration
	// InitialTimeout is used until enough observations accumulate.
	// Default: 5s.
	InitialTimeout time.Duration
}

// defaultAdaptiveTimeoutConfig returns sensible defaults for adaptive timeout.
func defaultAdaptiveTimeoutConfig() *AdaptiveTimeoutConfig {
	return &AdaptiveTimeoutConfig{
		Percentile:     0.95,
		Multiplier:     2.0,
		WindowSize:     100,
		MinTimeout:     100 * time.Millisecond,
		MaxTimeout:     30 * time.Second,
		InitialTimeout: 5 * time.Second,
	}
}

// adaptiveTimeoutTracker tracks recent response latencies and computes
// adaptive timeouts based on a percentile of observed latencies.
// It is thread-safe.
type adaptiveTimeoutTracker struct {
	mu           sync.RWMutex
	cfg          *AdaptiveTimeoutConfig
	observations []time.Duration // circular buffer (slice with capacity > len)
	count        int             // total number of observations ever recorded
}

// newAdaptiveTimeoutTracker creates a new tracker with the given config.
func newAdaptiveTimeoutTracker(cfg *AdaptiveTimeoutConfig) *adaptiveTimeoutTracker {
	if cfg == nil {
		cfg = defaultAdaptiveTimeoutConfig()
	}
	return &adaptiveTimeoutTracker{
		cfg:          cfg,
		observations: make([]time.Duration, 0, cfg.WindowSize),
	}
}

// record adds a new latency observation to the window.
func (t *adaptiveTimeoutTracker) record(latency time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.observations) < t.cfg.WindowSize {
		t.observations = append(t.observations, latency)
	} else {
		// Overwrite oldest in circular fashion.
		idx := t.count % t.cfg.WindowSize
		t.observations[idx] = latency
	}
	t.count++
}

// computeTimeout calculates the adaptive timeout based on current observations.
// Falls back to InitialTimeout if fewer than 5 observations have been recorded.
func (t *adaptiveTimeoutTracker) computeTimeout() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// Require a minimum number of observations before using adaptive timeout.
	if len(t.observations) < 5 {
		return t.cfg.InitialTimeout
	}

	// Sort a copy of the observations to find the percentile.
	sorted := make([]time.Duration, len(t.observations))
	copy(sorted, t.observations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	// Calculate percentile index.
	idx := int(float64(len(sorted)-1) * t.cfg.Percentile)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	if idx < 0 {
		idx = 0
	}

	percentileLatency := sorted[idx]

	// Apply multiplier.
	timeout := time.Duration(float64(percentileLatency) * t.cfg.Multiplier)

	// Clamp between min and max.
	if timeout < t.cfg.MinTimeout {
		timeout = t.cfg.MinTimeout
	}
	if timeout > t.cfg.MaxTimeout {
		timeout = t.cfg.MaxTimeout
	}

	return timeout
}
