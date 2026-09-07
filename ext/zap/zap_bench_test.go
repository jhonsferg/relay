package zap_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	uberzap "go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/jhonsferg/relay"
	relayzap "github.com/jhonsferg/relay/ext/zap"
)

// newDiscardLogger builds a *zap.Logger with a real JSON encoder writing to
// io.Discard, so the benchmark measures the adapter's forwarding overhead
// (SugaredLogger field handling/encoding) rather than I/O.
func newDiscardLogger() *uberzap.Logger {
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(uberzap.NewProductionEncoderConfig()),
		zapcore.AddSync(io.Discard),
		zapcore.DebugLevel,
	)
	return uberzap.New(core)
}

// BenchmarkZapAdapter_RequestLogging measures the per-request overhead of
// routing relay's Debug/Warn log calls (emitted by relay's
// [relay.WithRequestLogger] transport middleware on every request/response)
// through the zap adapter, end to end through a relay.Client.
func BenchmarkZapAdapter_RequestLogging(b *testing.B) {
	adapter := relayzap.NewAdapter(newDiscardLogger())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithRequestLogger(adapter),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Get("/bench"))
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("unexpected status %d", resp.StatusCode)
		}
		relay.PutResponse(resp)
	}
}

// BenchmarkZapAdapter_Debug isolates the adapter's Debug call (bypassing the
// relay.Client/transport stack) to measure the raw SugaredLogger forwarding
// cost with a realistic key/value field set.
func BenchmarkZapAdapter_Debug(b *testing.B) {
	adapter := relayzap.NewAdapter(newDiscardLogger())

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		adapter.Debug("relay: request",
			"method", "GET",
			"url", "https://api.example.com/bench",
			"status", 200,
			"latency_ms", int64(12),
		)
	}
}
