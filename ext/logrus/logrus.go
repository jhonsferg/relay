// Package logrus provides a github.com/sirupsen/logrus adapter implementing
// [relay.Logger]. It supports both *logrus.Logger and *logrus.Entry (which
// carries pre-set fields).
//
// Usage:
//
//	import (
//	    "github.com/sirupsen/logrus"
//	    "github.com/jhonsferg/relay"
//	    relaylogrus "github.com/jhonsferg/relay/ext/logrus"
//	)
//
//	log := logrus.New()
//	log.SetLevel(logrus.DebugLevel)
//	log.SetFormatter(&logrus.JSONFormatter{})
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relay.WithLogger(relaylogrus.NewAdapter(log)),
//	)
//
// # Pre-set fields with Entry
//
// Use [NewEntryAdapter] to attach static context fields (service name, version,
// request-id, etc.) that appear on every relay log line:
//
//	entry := logrus.WithFields(logrus.Fields{
//	    "service": "order-service",
//	    "version": "2.1.0",
//	})
//	client := relay.New(relay.WithLogger(relaylogrus.NewEntryAdapter(entry)))
//
// # Key/value pairs
//
// relay emits structured log calls with alternating key/value pairs
// (matching log/slog convention):
//
//	logger.Info("retry", "attempt", 2, "wait_ms", 400)
//
// Both adapters convert these pairs to logrus.Fields automatically. Odd
// trailing arguments (unpaired keys) are included under the key "EXTRA".
//
// # Migration
//
// Replace this package with [relay.SlogAdapter] from the core relay module,
// which wraps any *log/slog.Logger and requires no third-party dependency:
//
//	import (
//	    "log/slog"
//	    "github.com/jhonsferg/relay"
//	)
//
//	client := relay.New(
//	    relay.WithLogger(relay.SlogAdapter(slog.Default())),
//	)
//
// Deprecated: This package is deprecated and will be removed in relay v2.0.
// Migrate to [relay.SlogAdapter] or [github.com/jhonsferg/relay/ext/slog],
// which integrate with Go's standard log/slog package (Go 1.21+).
package logrus

import (
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/jhonsferg/relay"
)

// logrusAdapter wraps a *logrus.Logger.
type logrusAdapter struct{ l *logrus.Logger }

// logrusEntryAdapter wraps a *logrus.Entry with pre-set fields.
type logrusEntryAdapter struct{ e *logrus.Entry }

// NewAdapter returns a [relay.Logger] backed by l.
//
// Deprecated: Use [relay.SlogAdapter] with a *log/slog.Logger instead.
// This function will be removed in relay v2.0.
func NewAdapter(l *logrus.Logger) relay.Logger { return &logrusAdapter{l: l} }

// NewEntryAdapter returns a [relay.Logger] backed by e. All relay log lines
// will carry the fields already set on e.
//
// Deprecated: Use [relay.SlogAdapter] with a *log/slog.Logger instead.
// This function will be removed in relay v2.0.
func NewEntryAdapter(e *logrus.Entry) relay.Logger { return &logrusEntryAdapter{e: e} }

// -- logrusAdapter -------------------------------------------------------------

func (a *logrusAdapter) Debug(msg string, args ...any) {
	a.l.WithFields(toFields(args)).Debug(msg)
}

func (a *logrusAdapter) Info(msg string, args ...any) {
	a.l.WithFields(toFields(args)).Info(msg)
}

func (a *logrusAdapter) Warn(msg string, args ...any) {
	a.l.WithFields(toFields(args)).Warn(msg)
}

func (a *logrusAdapter) Error(msg string, args ...any) {
	a.l.WithFields(toFields(args)).Error(msg)
}

// -- logrusEntryAdapter --------------------------------------------------------

func (a *logrusEntryAdapter) Debug(msg string, args ...any) {
	a.e.WithFields(toFields(args)).Debug(msg)
}

func (a *logrusEntryAdapter) Info(msg string, args ...any) {
	a.e.WithFields(toFields(args)).Info(msg)
}

func (a *logrusEntryAdapter) Warn(msg string, args ...any) {
	a.e.WithFields(toFields(args)).Warn(msg)
}

func (a *logrusEntryAdapter) Error(msg string, args ...any) {
	a.e.WithFields(toFields(args)).Error(msg)
}

// -- helpers -------------------------------------------------------------------

// toFields converts alternating key/value pairs to a logrus.Fields map.
// Non-string keys are converted to their string representation via fmt.Sprintf.
// An unpaired trailing key is stored under "EXTRA".
func toFields(args []any) logrus.Fields {
	fields := make(logrus.Fields, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		key := ""
		switch k := args[i].(type) {
		case string:
			key = k
		default:
			key = fmt.Sprintf("%v", k) // rarely needed
		}
		fields[key] = args[i+1]
	}
	if len(args)%2 != 0 {
		fields["EXTRA"] = args[len(args)-1]
	}
	return fields
}
