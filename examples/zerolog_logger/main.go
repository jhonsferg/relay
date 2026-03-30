// Package main demonstrates using github.com/rs/zerolog as the structured
// logger for a relay HTTP client via github.com/jhonsferg/relay/ext/zerolog.
//
// zerolog is a zero-allocation JSON logger. relay emits internal log events as
// alternating key/value pairs, which the adapter forwards directly to zerolog's
// event.Fields() - keeping allocations minimal.
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/rs/zerolog"

	relay "github.com/jhonsferg/relay"
	relayzl "github.com/jhonsferg/relay/ext/zerolog"
)

func main() {
	// ---------------------------------------------------------------------------
	// 1. Build a zerolog logger.
	//
	// zerolog.ConsoleWriter formats JSON as human-readable coloured text for
	// development. In production you would write directly to os.Stdout (or a
	// rolling file) to get raw JSON.
	// ---------------------------------------------------------------------------
	consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	logger := zerolog.New(consoleWriter).
		With().
		Timestamp().
		Str("component", "relay").
		Str("service", "example-svc").
		Logger().
		Level(zerolog.DebugLevel)

	// ---------------------------------------------------------------------------
	// 2. Test server: responds with different bodies based on path so we can
	// observe the relay debug logs per request.
	// ---------------------------------------------------------------------------
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"path":%q,"method":%q}`, r.URL.Path, r.Method)
	}))
	defer srv.Close()

	// ---------------------------------------------------------------------------
	// 3. Wire the zerolog adapter into a relay client.
	//
	// relayzl.NewAdapter copies the logger by value - later mutations to
	// `logger` do not affect the client.
	// ---------------------------------------------------------------------------
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithLogger(relayzl.NewAdapter(logger)),
		relay.WithTimeout(10*time.Second),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	fmt.Println("=== GET request (observe zerolog output below) ===")
	resp, err := client.Execute(client.Get("/users/7"))
	if err != nil {
		log.Fatalf("Execute: %v", err)
	}
	fmt.Printf("\nResponse: %d - %s\n\n", resp.StatusCode, resp.String())

	// ---------------------------------------------------------------------------
	// 4. Sub-logger with additional context fields.
	//
	// zerolog loggers can be derived with .With().Str(...).Logger(). The extra
	// fields appear on every relay log line emitted through this client.
	// ---------------------------------------------------------------------------
	fmt.Println("=== Sub-logger with context fields ===")
	subLogger := logger.With().
		Str("subsystem", "orders").
		Int("merchant_id", 42).
		Logger()

	client2 := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithLogger(relayzl.NewAdapter(subLogger)),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	resp2, err := client2.Execute(client2.Post("/orders").
		WithJSON(map[string]any{"item": "widget", "qty": 3}))
	if err != nil {
		log.Fatalf("Execute client2: %v", err)
	}
	fmt.Printf("\nResponse: %d - %s\n\n", resp2.StatusCode, resp2.String())

	// ---------------------------------------------------------------------------
	// 5. Level filtering - only Warn and above.
	//
	// Useful in production to suppress per-request Debug noise without removing
	// the logger entirely.
	// ---------------------------------------------------------------------------
	fmt.Println("=== Level-filtered client (warn+) ===")
	warnLogger := zerolog.New(consoleWriter).
		With().Timestamp().Logger().
		Level(zerolog.WarnLevel)

	client3 := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithLogger(relayzl.NewAdapter(warnLogger)),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	resp3, err := client3.Execute(client3.Get("/silent"))
	if err != nil {
		log.Fatalf("Execute client3: %v", err)
	}
	// Debug/Info entries from relay are suppressed; only Warn+ would appear.
	fmt.Printf("Response: %d (no debug logs emitted above)\n", resp3.StatusCode)

	// ---------------------------------------------------------------------------
	// 6. JSON production logger (writes raw JSON to stdout).
	// ---------------------------------------------------------------------------
	fmt.Println("\n=== JSON production logger ===")
	jsonLogger := zerolog.New(os.Stdout).
		With().
		Timestamp().
		Str("env", "production").
		Logger().
		Level(zerolog.InfoLevel)

	client4 := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithLogger(relayzl.NewAdapter(jsonLogger)),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	resp4, err := client4.Execute(client4.Get("/health"))
	if err != nil {
		log.Fatalf("Execute client4: %v", err)
	}
	fmt.Printf("\nResponse: %d - %s\n", resp4.StatusCode, resp4.String())
}
