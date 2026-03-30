package relay

import (
	"context"
	"net/http"
	"time"
)

// HealthCheckConfig controls the background health-probe goroutine that
// proactively tests whether a tripped circuit breaker should be reset.
//
// When the circuit breaker is Open, the relay client normally waits for
// [CircuitBreakerConfig.ResetTimeout] to elapse before it allows a probe
// request through. WithHealthCheck shortens this recovery time by actively
// polling a dedicated health endpoint in the background and resetting the
// breaker as soon as the endpoint responds with a healthy status.
type HealthCheckConfig struct {
	// URL is the HTTP endpoint to probe. It must be a fully-qualified URL
	// (e.g. "https://api.example.com/health"). The client issues a plain GET
	// using the standard library - not through the relay pipeline - so the
	// probe is not subject to the main circuit breaker, retries, or rate limiter.
	URL string

	// Interval is how often to send a probe while the circuit is Open.
	// Smaller values recover faster at the cost of more background traffic.
	// A reasonable default is 10–30 seconds.
	Interval time.Duration

	// Timeout is the per-probe HTTP deadline. It should be shorter than
	// Interval so a slow probe does not delay the next one.
	Timeout time.Duration

	// ExpectedStatus is the HTTP status code that signals a healthy upstream.
	// Any 2xx status is accepted when ExpectedStatus is 0.
	ExpectedStatus int
}

// runHealthCheck is the background goroutine started when [WithHealthCheck] is
// configured. It polls cfg.URL every cfg.Interval while the circuit breaker is
// Open and resets it on a matching response. The goroutine exits when ctx is
// canceled (i.e. when [Client.Shutdown] is called).
func (c *Client) runHealthCheck(ctx context.Context, cfg *HealthCheckConfig) {
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	probeClient := &http.Client{Timeout: cfg.Timeout}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Only probe when the circuit is actually open - no-op otherwise.
			if c.CircuitBreakerState() != StateOpen {
				continue
			}
			if c.probeHealthEndpoint(ctx, probeClient, cfg) {
				c.ResetCircuitBreaker()
				c.config.Logger.Info("health check: circuit breaker reset after successful probe",
					"url", cfg.URL,
				)
			}
		}
	}
}

// probeHealthEndpoint sends a single GET to cfg.URL and returns true if the
// response status satisfies cfg.ExpectedStatus (or any 2xx when it is 0).
func (c *Client) probeHealthEndpoint(ctx context.Context, hc *http.Client, cfg *HealthCheckConfig) bool {
	probeCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, cfg.URL, nil)
	if err != nil {
		return false
	}

	resp, err := hc.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()

	if cfg.ExpectedStatus != 0 {
		return resp.StatusCode == cfg.ExpectedStatus
	}
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
