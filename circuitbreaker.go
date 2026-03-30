package relay

import (
	"sync"
	"time"
)

// CircuitBreakerState represents the current state of the circuit breaker.
type CircuitBreakerState int

const (
	// StateClosed is the normal operating state. All requests pass through and
	// failures are counted. When failures reach MaxFailures the breaker trips
	// to StateOpen.
	StateClosed CircuitBreakerState = iota

	// StateHalfOpen is the recovery probe state. A limited number of requests
	// are allowed through to test whether the downstream has recovered. On
	// enough consecutive successes the breaker transitions to StateClosed; on
	// any failure it returns to StateOpen.
	StateHalfOpen

	// StateOpen is the tripped state. All requests are rejected immediately
	// with [ErrCircuitOpen] without reaching the network. After ResetTimeout
	// the breaker transitions to StateHalfOpen automatically.
	StateOpen
)

// String returns the human-readable name of the state.
func (s CircuitBreakerState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half-open"
	case StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig controls the thresholds and behaviour of the breaker.
type CircuitBreakerConfig struct {
	// MaxFailures is the number of consecutive failures in StateClosed that
	// trips the breaker to StateOpen. Must be > 0.
	MaxFailures int

	// ResetTimeout is how long the breaker stays in StateOpen before
	// automatically transitioning to StateHalfOpen to probe recovery.
	ResetTimeout time.Duration

	// HalfOpenRequests is the maximum number of probe requests allowed while
	// in StateHalfOpen. Additional requests are rejected until the breaker
	// decides to close or re-open.
	HalfOpenRequests int

	// SuccessThreshold is the number of consecutive successes required while
	// in StateHalfOpen to transition back to StateClosed.
	SuccessThreshold int

	// OnStateChange is an optional callback invoked on every state transition.
	// It receives the previous and new states. The callback is invoked OUTSIDE
	// the breaker's internal mutex - it is safe to call breaker methods from
	// within it.
	OnStateChange func(from, to CircuitBreakerState)
}

// defaultCircuitBreakerConfig returns a CircuitBreakerConfig with conservative
// production defaults: trips after 5 failures, probes after 60 s, closes after
// 2 consecutive successes in half-open.
func defaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		MaxFailures:      5,
		ResetTimeout:     60 * time.Second,
		HalfOpenRequests: 3,
		SuccessThreshold: 2,
	}
}

// CircuitBreaker implements the three-state circuit-breaker pattern
// (Closed → Open → Half-Open → Closed). It is safe for concurrent use.
type CircuitBreaker struct {
	// mu protects all mutable state below.
	mu sync.Mutex

	// config is the immutable configuration set at construction time.
	config *CircuitBreakerConfig

	// state is the current circuit breaker state.
	state CircuitBreakerState

	// failures counts consecutive failures while in StateClosed.
	failures int

	// successes counts consecutive successes while in StateHalfOpen.
	successes int

	// halfOpenRequests tracks how many probe requests have been dispatched
	// while in StateHalfOpen.
	halfOpenRequests int

	// lastFailureTime records when the most recent failure occurred. Used to
	// determine when the ResetTimeout has elapsed in StateOpen.
	lastFailureTime time.Time
}

// newCircuitBreaker constructs a CircuitBreaker from cfg. If cfg is nil the
// default configuration is used.
func newCircuitBreaker(cfg *CircuitBreakerConfig) *CircuitBreaker {
	if cfg == nil {
		cfg = defaultCircuitBreakerConfig()
	}
	return &CircuitBreaker{
		config: cfg,
		state:  StateClosed,
	}
}

// transition changes the breaker state and fires OnStateChange when the new
// state differs from the old one. Must be called with cb.mu held.
// The OnStateChange callback is invoked AFTER releasing the mutex to prevent
// deadlocks if the callback re-enters the circuit breaker.
func (cb *CircuitBreaker) transition(to CircuitBreakerState) {
	from := cb.state
	cb.state = to
	if cb.config.OnStateChange != nil && from != to {
		fn := cb.config.OnStateChange
		cb.mu.Unlock()
		fn(from, to)
		cb.mu.Lock()
	}
}

// Allow reports whether a request should be attempted given the current state.
// In StateOpen it transitions to StateHalfOpen when the reset timeout has
// elapsed. In StateHalfOpen it limits concurrent probes to HalfOpenRequests.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.lastFailureTime) > cb.config.ResetTimeout {
			cb.transition(StateHalfOpen)
			cb.halfOpenRequests = 0
			cb.successes = 0
			return true
		}
		return false
	case StateHalfOpen:
		if cb.halfOpenRequests < cb.config.HalfOpenRequests {
			cb.halfOpenRequests++
			return true
		}
		return false
	}
	return false
}

// RecordSuccess records a successful response. In StateClosed it resets the
// failure counter. In StateHalfOpen it increments the success counter and
// transitions to StateClosed once SuccessThreshold is reached.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		cb.failures = 0
	case StateHalfOpen:
		cb.successes++
		if cb.successes >= cb.config.SuccessThreshold {
			cb.transition(StateClosed)
			cb.failures = 0
			cb.successes = 0
		}
	}
}

// RecordFailure records a failed response or transport error. In StateClosed
// it increments the failure counter and trips to StateOpen when MaxFailures is
// reached. In StateHalfOpen a single failure immediately re-opens the breaker.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()
	switch cb.state {
	case StateClosed:
		cb.failures++
		if cb.failures >= cb.config.MaxFailures {
			cb.transition(StateOpen)
		}
	case StateHalfOpen:
		cb.transition(StateOpen)
		cb.halfOpenRequests = 0
		cb.successes = 0
	}
}

// State returns the current CircuitBreakerState without modifying any counters.
func (cb *CircuitBreaker) State() CircuitBreakerState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// Reset forces the breaker back to StateClosed and clears all counters.
// Useful after a manual health check confirms that the downstream has recovered.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.transition(StateClosed)
	cb.failures = 0
	cb.successes = 0
	cb.halfOpenRequests = 0
}
