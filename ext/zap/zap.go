// Package zap provides a go.uber.org/zap adapter for the relay HTTP client
// logger interface. It lets applications that already use zap plug their logger
// directly into relay without any intermediate conversion layer.
//
// Usage:
//
//	import (
//	    "go.uber.org/zap"
//	    "github.com/jhonsferg/relay"
//	    relayzap "github.com/jhonsferg/relay/ext/zap"
//	)
//
//	logger, _ := zap.NewProduction()
//	defer logger.Sync()
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relay.WithLogger(relayzap.NewAdapter(logger)),
//	)
//
// relay passes structured log fields as alternating key/value pairs — the same
// convention as go.uber.org/zap's SugaredLogger.Debugw / Infow / Warnw /
// Errorw family. The adapter forwards them without any extra allocation.
package zap

import (
	"go.uber.org/zap"

	"github.com/jhonsferg/relay"
)

// zapAdapter wraps a *zap.SugaredLogger to satisfy the relay.Logger interface.
type zapAdapter struct {
	s *zap.SugaredLogger
}

// NewAdapter wraps l so it can be passed to [relay.WithLogger].
// The underlying SugaredLogger is used so that the key/value pairs relay
// passes as variadic args are forwarded without allocating zap.Field values.
func NewAdapter(l *zap.Logger) relay.Logger {
	return &zapAdapter{s: l.Sugar()}
}

// NewSugaredAdapter wraps an already-sugared logger. Use this when your
// application works directly with *zap.SugaredLogger.
func NewSugaredAdapter(s *zap.SugaredLogger) relay.Logger {
	return &zapAdapter{s: s}
}

func (a *zapAdapter) Debug(msg string, args ...any) { a.s.Debugw(msg, args...) }
func (a *zapAdapter) Info(msg string, args ...any)  { a.s.Infow(msg, args...) }
func (a *zapAdapter) Warn(msg string, args ...any)  { a.s.Warnw(msg, args...) }
func (a *zapAdapter) Error(msg string, args ...any) { a.s.Errorw(msg, args...) }
