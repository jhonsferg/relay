package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAdaptiveTimeoutTracker_Record(t *testing.T) {
	t.Parallel()
	cfg := &AdaptiveTimeoutConfig{
		WindowSize:     5,
		Percentile:     0.95,
		Multiplier:     2.0,
		MinTimeout:     100 * time.Millisecond,
		MaxTimeout:     30 * time.Second,
		InitialTimeout: 5 * time.Second,
	}
	tracker := newAdaptiveTimeoutTracker(cfg)

	// Record some latencies.
	latencies := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 150 * time.Millisecond}
	for _, l := range latencies {
		tracker.record(l)
	}

	tracker.mu.RLock()
	if len(tracker.observations) != 3 {
		t.Errorf("expected 3 observations, got %d", len(tracker.observations))
	}
	if tracker.count != 3 {
		t.Errorf("expected count 3, got %d", tracker.count)
	}
	tracker.mu.RUnlock()
}

func TestAdaptiveTimeoutTracker_CircularBuffer(t *testing.T) {
	t.Parallel()
	cfg := &AdaptiveTimeoutConfig{
		WindowSize:     3,
		Percentile:     0.95,
		Multiplier:     2.0,
		MinTimeout:     100 * time.Millisecond,
		MaxTimeout:     30 * time.Second,
		InitialTimeout: 5 * time.Second,
	}
	tracker := newAdaptiveTimeoutTracker(cfg)

	// Record 5 latencies in a window of size 3.
	for i := 0; i < 5; i++ {
		tracker.record(time.Duration(i*100) * time.Millisecond)
	}

	tracker.mu.RLock()
	// Should only have 3 observations (the last 3: 200ms, 300ms, 400ms).
	if len(tracker.observations) != 3 {
		t.Errorf("expected 3 observations after wraparound, got %d", len(tracker.observations))
	}
	if tracker.count != 5 {
		t.Errorf("expected count 5, got %d", tracker.count)
	}
	tracker.mu.RUnlock()
}

func TestAdaptiveTimeoutTracker_ComputeTimeout_InsufficientObservations(t *testing.T) {
	t.Parallel()
	cfg := &AdaptiveTimeoutConfig{
		WindowSize:     100,
		Percentile:     0.95,
		Multiplier:     2.0,
		MinTimeout:     100 * time.Millisecond,
		MaxTimeout:     30 * time.Second,
		InitialTimeout: 5 * time.Second,
	}
	tracker := newAdaptiveTimeoutTracker(cfg)

	// Record only 4 observations.
	for i := 0; i < 4; i++ {
		tracker.record(100 * time.Millisecond)
	}

	// Should return InitialTimeout.
	timeout := tracker.computeTimeout()
	if timeout != cfg.InitialTimeout {
		t.Errorf("expected InitialTimeout %v, got %v", cfg.InitialTimeout, timeout)
	}
}

func TestAdaptiveTimeoutTracker_ComputeTimeout_Percentile(t *testing.T) {
	t.Parallel()
	cfg := &AdaptiveTimeoutConfig{
		WindowSize:     100,
		Percentile:     0.95,
		Multiplier:     2.0,
		MinTimeout:     100 * time.Millisecond,
		MaxTimeout:     30 * time.Second,
		InitialTimeout: 5 * time.Second,
	}
	tracker := newAdaptiveTimeoutTracker(cfg)

	// Record 10 latencies: 100ms, 110ms, ..., 190ms
	for i := 0; i < 10; i++ {
		latency := time.Duration(100+i*10) * time.Millisecond
		tracker.record(latency)
	}

	timeout := tracker.computeTimeout()

	// p95 of [100, 110, ..., 190] is at index 8 (int(9*0.95)=8), which is 180ms.
	// With multiplier 2.0, expected timeout = 180ms * 2.0 = 360ms.
	expected := 360 * time.Millisecond
	if timeout != expected {
		t.Errorf("expected %v, got %v", expected, timeout)
	}
}

func TestAdaptiveTimeoutTracker_ComputeTimeout_Clamping(t *testing.T) {
	t.Parallel()

	t.Run("clamp to min", func(t *testing.T) {
		cfg := &AdaptiveTimeoutConfig{
			WindowSize:     100,
			Percentile:     0.95,
			Multiplier:     0.1, // very low multiplier
			MinTimeout:     500 * time.Millisecond,
			MaxTimeout:     30 * time.Second,
			InitialTimeout: 5 * time.Second,
		}
		tracker := newAdaptiveTimeoutTracker(cfg)

		// Record 5 observations of 10ms each.
		for i := 0; i < 5; i++ {
			tracker.record(10 * time.Millisecond)
		}

		timeout := tracker.computeTimeout()
		// p95 of [10ms, 10ms, 10ms, 10ms, 10ms] = 10ms.
		// With multiplier 0.1 = 1ms. Clamped to MinTimeout 500ms.
		if timeout != cfg.MinTimeout {
			t.Errorf("expected MinTimeout %v, got %v", cfg.MinTimeout, timeout)
		}
	})

	t.Run("clamp to max", func(t *testing.T) {
		cfg := &AdaptiveTimeoutConfig{
			WindowSize:     100,
			Percentile:     0.95,
			Multiplier:     100.0, // very high multiplier
			MinTimeout:     100 * time.Millisecond,
			MaxTimeout:     2 * time.Second,
			InitialTimeout: 5 * time.Second,
		}
		tracker := newAdaptiveTimeoutTracker(cfg)

		// Record 5 observations of 1 second each.
		for i := 0; i < 5; i++ {
			tracker.record(1 * time.Second)
		}

		timeout := tracker.computeTimeout()
		// p95 of [1s, 1s, 1s, 1s, 1s] = 1s.
		// With multiplier 100.0 = 100s. Clamped to MaxTimeout 2s.
		if timeout != cfg.MaxTimeout {
			t.Errorf("expected MaxTimeout %v, got %v", cfg.MaxTimeout, timeout)
		}
	})
}

func TestAdaptiveTimeoutTracker_ThreadSafety(t *testing.T) {
	t.Parallel()
	cfg := &AdaptiveTimeoutConfig{
		WindowSize:     1000,
		Percentile:     0.95,
		Multiplier:     2.0,
		MinTimeout:     100 * time.Millisecond,
		MaxTimeout:     30 * time.Second,
		InitialTimeout: 5 * time.Second,
	}
	tracker := newAdaptiveTimeoutTracker(cfg)

	// Spawn goroutines that concurrently record and compute.
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				tracker.record(time.Duration(id*100+j) * time.Millisecond)
				_ = tracker.computeTimeout()
			}
			done <- struct{}{}
		}(i)
	}

	// Wait for all goroutines.
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify the tracker is still functional.
	timeout := tracker.computeTimeout()
	if timeout <= 0 {
		t.Errorf("expected positive timeout, got %v", timeout)
	}
}

func TestIntegration_AdaptiveTimeout(t *testing.T) {
	t.Parallel()

	// Create a test server with predictable latencies.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer server.Close()

	cfg := &AdaptiveTimeoutConfig{
		WindowSize:     100,
		Percentile:     0.95,
		Multiplier:     2.0,
		MinTimeout:     100 * time.Millisecond,
		MaxTimeout:     30 * time.Second,
		InitialTimeout: 5 * time.Second,
	}

	client := New(
		WithBaseURL(server.URL),
		WithAdaptiveTimeout(*cfg),
	)
	defer func() {
		_ = client.Shutdown(context.Background())
	}()

	// Execute multiple requests to build up the observation window.
	for i := 0; i < 10; i++ {
		req := client.Get("/")
		_, err := client.Execute(req)
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
	}

	// Verify the adaptive timeout was computed and applied.
	// This is more of a smoke test; detailed timing validation would require
	// mocking at a lower level.
	if client.adaptiveTimeout == nil {
		t.Error("expected adaptiveTimeout to be set on client")
	}
}

func TestAdaptiveTimeoutTracker_Nil_Config(t *testing.T) {
	t.Parallel()
	// Passing nil config should use defaults.
	tracker := newAdaptiveTimeoutTracker(nil)
	if tracker.cfg == nil {
		t.Error("expected defaults to be set for nil config")
	}
	if tracker.cfg.Percentile != 0.95 {
		t.Errorf("expected default percentile 0.95, got %v", tracker.cfg.Percentile)
	}
}

func TestAdaptiveTimeoutTracker_EdgeCase_OneObservation(t *testing.T) {
	t.Parallel()
	cfg := &AdaptiveTimeoutConfig{
		WindowSize:     100,
		Percentile:     0.5,
		Multiplier:     1.0,
		MinTimeout:     100 * time.Millisecond,
		MaxTimeout:     30 * time.Second,
		InitialTimeout: 5 * time.Second,
	}
	tracker := newAdaptiveTimeoutTracker(cfg)
	tracker.record(100 * time.Millisecond)

	// Still fewer than 5 observations, should return InitialTimeout.
	timeout := tracker.computeTimeout()
	if timeout != cfg.InitialTimeout {
		t.Errorf("expected InitialTimeout with 1 observation, got %v", timeout)
	}
}

func TestAdaptiveTimeoutTracker_EdgeCase_ExactlyFiveObservations(t *testing.T) {
	t.Parallel()
	cfg := &AdaptiveTimeoutConfig{
		WindowSize:     100,
		Percentile:     0.5, // median
		Multiplier:     1.0,
		MinTimeout:     100 * time.Millisecond,
		MaxTimeout:     30 * time.Second,
		InitialTimeout: 5 * time.Second,
	}
	tracker := newAdaptiveTimeoutTracker(cfg)

	// Record exactly 5 observations: 100ms, 200ms, 300ms, 400ms, 500ms
	for i := 1; i <= 5; i++ {
		tracker.record(time.Duration(i*100) * time.Millisecond)
	}

	timeout := tracker.computeTimeout()
	// p50 of [100, 200, 300, 400, 500] is 300ms (index 2).
	// With multiplier 1.0 = 300ms.
	expected := 300 * time.Millisecond
	if timeout != expected {
		t.Errorf("expected %v, got %v", expected, timeout)
	}
}
