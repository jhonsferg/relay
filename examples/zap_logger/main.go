// Package main demonstrates using go.uber.org/zap as the structured logger for
// a relay HTTP client via github.com/jhonsferg/relay/ext/zap.
//
// relay emits internal log events (request started, retry attempt, circuit
// breaker state change, etc.) at the Debug, Info, Warn, and Error levels. Any
// *zap.Logger — development, production, or custom — can be plugged in with
// a single option.
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	uberzap "go.uber.org/zap"

	relay "github.com/jhonsferg/relay"
	relayzap "github.com/jhonsferg/relay/ext/zap"
)

func main() {
	// ---------------------------------------------------------------------------
	// 1. Build a zap logger.
	//
	// zap.NewDevelopment() produces human-readable, coloured console output
	// ideal for local development. Switch to zap.NewProduction() for structured
	// JSON in staging / production.
	// ---------------------------------------------------------------------------
	logger, err := uberzap.NewDevelopment()
	if err != nil {
		log.Fatalf("zap.NewDevelopment: %v", err)
	}
	defer logger.Sync() //nolint:errcheck

	// Optionally add static fields that appear on every relay log line.
	logger = logger.With(
		uberzap.String("component", "relay"),
		uberzap.String("service", "example-svc"),
	)

	// ---------------------------------------------------------------------------
	// 2. Test server: deliberate 500 on the first call so relay retries and we
	// can observe the retry log entries from the relay client.
	// ---------------------------------------------------------------------------
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"error":"transient failure"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"call":%d,"path":%q}`, callCount, r.URL.Path)
	}))
	defer srv.Close()

	// ---------------------------------------------------------------------------
	// 3. Build the relay client, wiring in the zap adapter.
	//
	// relayzap.NewAdapter wraps *zap.Logger into relay.Logger by delegating to
	// the SugaredLogger.Debugw / Infow / Warnw / Errorw methods.
	// ---------------------------------------------------------------------------
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithLogger(relayzap.NewAdapter(logger)),
		relay.WithTimeout(10*time.Second),
		relay.WithRetry(&relay.RetryConfig{
			MaxAttempts:     3,
			InitialInterval: 10 * time.Millisecond,
			Multiplier:      2.0,
			MaxInterval:     100 * time.Millisecond,
			RetryableStatus: []int{500, 502, 503, 504},
		}),
		relay.WithDisableCircuitBreaker(),
	)

	fmt.Println("=== Request with retry (observe zap output below) ===")
	resp, err := client.Execute(client.Get("/items/42"))
	if err != nil {
		log.Fatalf("Execute: %v", err)
	}
	fmt.Printf("\nFinal response: %d — %s\n", resp.StatusCode, resp.String())

	// ---------------------------------------------------------------------------
	// 4. Named child logger per request context.
	//
	// Use logger.Named or logger.With to scope fields to a subsystem. relay's
	// adapter forwards whatever context you attach.
	// ---------------------------------------------------------------------------
	fmt.Println("\n=== Named child logger ===")
	namedLogger := logger.Named("payments").With(uberzap.String("currency", "USD"))

	client2 := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithLogger(relayzap.NewAdapter(namedLogger)),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	resp2, err := client2.Execute(client2.Get("/payments/99"))
	if err != nil {
		log.Fatalf("Execute client2: %v", err)
	}
	fmt.Printf("Final response: %d — %s\n", resp2.StatusCode, resp2.String())

	// ---------------------------------------------------------------------------
	// 5. SugaredLogger variant.
	//
	// If your application already uses *zap.SugaredLogger, pass it directly
	// without converting back to *zap.Logger.
	// ---------------------------------------------------------------------------
	fmt.Println("\n=== SugaredLogger variant ===")
	sugared := logger.Sugar()
	client3 := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithLogger(relayzap.NewSugaredAdapter(sugared)),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	resp3, err := client3.Execute(client3.Get("/sugared/path"))
	if err != nil {
		log.Fatalf("Execute client3: %v", err)
	}
	fmt.Printf("Final response: %d — %s\n", resp3.StatusCode, resp3.String())
}
