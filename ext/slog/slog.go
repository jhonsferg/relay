// Package slog provides structured HTTP request and response logging for the
// relay HTTP client using Go's standard log/slog package (available since
// Go 1.21).
//
// Usage:
//
//	import (
//	    "log/slog"
//	    "github.com/jhonsferg/relay"
//	    relayslog "github.com/jhonsferg/relay/ext/slog"
//	)
//
//	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relayslog.WithRequestResponseLogging(logger),
//	)
//
// # Logging levels
//
// Logs are emitted at different slog levels based on HTTP response status:
//   - LevelInfo (2xx, 3xx responses)
//   - LevelWarn (4xx client errors)
//   - LevelError (5xx server errors and transport errors)
//
// # Log fields
//
// Each log entry includes structured fields:
//   - method: HTTP method (GET, POST, etc.)
//   - url: Request URL
//   - status_code: HTTP response status code (for successful responses)
//   - duration_ms: Request duration in milliseconds
//   - attempt: Retry attempt number (1-based, for BeforeRetryHook)
package slog

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/jhonsferg/relay"
)

// WithRequestResponseLogging returns a [relay.Option] that logs each HTTP
// request and response at the appropriate slog level. If logger is nil,
// [slog.Default] is used. Logs include method, url, status_code, and
// duration_ms fields.
func WithRequestResponseLogging(logger *slog.Logger) relay.Option {
	if logger == nil {
		logger = slog.Default()
	}

	return func(cfg *relay.Config) {
		cfg.OnAfterResponse = append(cfg.OnAfterResponse, func(ctx context.Context, resp *relay.Response) error {
			logResponse(logger, resp)
			return nil
		})

		cfg.BeforeRetryHooks = append(cfg.BeforeRetryHooks, func(ctx context.Context, attempt int, req *relay.Request, httpResp *http.Response, err error) {
			logRetry(logger, attempt, req, httpResp, err)
		})

		cfg.OnErrorHooks = append(cfg.OnErrorHooks, func(ctx context.Context, req *relay.Request, err error) {
			logError(logger, req, err)
		})
	}
}

func logResponse(logger *slog.Logger, resp *relay.Response) {
	duration := resp.Timing.Total.Milliseconds()

	level := slog.LevelInfo
	if resp.StatusCode >= 500 {
		level = slog.LevelError
	} else if resp.StatusCode >= 400 {
		level = slog.LevelWarn
	}

	method := ""
	url := ""
	if raw := resp.Raw(); raw != nil && raw.Request != nil {
		method = raw.Request.Method
		url = raw.Request.URL.String()
	}

	logger.Log(context.Background(), level, "http_response",
		"method", method,
		"url", url,
		"status_code", resp.StatusCode,
		"duration_ms", duration,
	)
}

func logRetry(logger *slog.Logger, attempt int, req *relay.Request, httpResp *http.Response, err error) {
	var statusCode int
	url := ""
	method := ""

	if httpResp != nil {
		statusCode = httpResp.StatusCode
		url = httpResp.Request.URL.String()
		method = httpResp.Request.Method
	} else if err != nil {
		logger.Log(context.Background(), slog.LevelError, "http_retry",
			"attempt", attempt,
			"error", err.Error(),
		)
		return
	}

	level := slog.LevelWarn
	if statusCode >= 500 {
		level = slog.LevelError
	}

	logger.Log(context.Background(), level, "http_retry",
		"attempt", attempt,
		"method", method,
		"url", url,
		"status_code", statusCode,
	)
}

func logError(logger *slog.Logger, req *relay.Request, err error) {
	if err == nil {
		return
	}

	logger.Log(context.Background(), slog.LevelError, "http_error",
		"method", req.Method(),
		"url", req.URL(),
		"error", err.Error(),
	)
}
