// Package main demonstrates relay's middleware and hook system:
//
//   - WithTransportMiddleware — wraps the http.RoundTripper for request/response
//     interception (logging, timing, header injection, response rewriting).
//   - WithOnBeforeRequest — hook called before each attempt (including retries).
//   - WithOnAfterResponse — hook called after a successful response.
//
// Middleware is applied outermost-last: the LAST added middleware is the FIRST
// to intercept a request and the LAST to see the response.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"time"

	relay "github.com/jhonsferg/relay"
)

// -- Middleware 1: request/response timing ------------------------------------

type timingMiddleware struct {
	base http.RoundTripper
}

func (m *timingMiddleware) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := m.base.RoundTrip(req)
	elapsed := time.Since(start)

	if err != nil {
		fmt.Printf("  [timing] %s %s → error in %s\n", req.Method, req.URL.Path, elapsed.Round(time.Microsecond))
	} else {
		fmt.Printf("  [timing] %s %s → %d in %s\n", req.Method, req.URL.Path, resp.StatusCode, elapsed.Round(time.Microsecond))
	}
	return resp, err
}

// -- Middleware 2: automatic request-ID injection ------------------------------

var requestCounter int

type requestIDMiddleware struct {
	base http.RoundTripper
}

func (m *requestIDMiddleware) RoundTrip(req *http.Request) (*http.Response, error) {
	requestCounter++
	// Clone the request so we don't mutate the original.
	clone := req.Clone(req.Context())
	clone.Header.Set("X-Request-Id", fmt.Sprintf("req-%04d", requestCounter))
	return m.base.RoundTrip(clone)
}

// -- Middleware 3: response body size guard ------------------------------------

type sizeguardMiddleware struct {
	base    http.RoundTripper
	limitMB int
}

func (m *sizeguardMiddleware) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := m.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.ContentLength > 0 && resp.ContentLength > int64(m.limitMB)*1024*1024 {
		resp.Body.Close()
		return nil, fmt.Errorf("response too large: %d bytes (limit %d MB)", resp.ContentLength, m.limitMB)
	}
	return resp, nil
}

func main() {
	// -------------------------------------------------------------------------
	// Test server — echoes request metadata back in the response body.
	// -------------------------------------------------------------------------
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"method":%q,"path":%q,"request_id":%q,"user_agent":%q}`,
			r.Method, r.URL.Path,
			r.Header.Get("X-Request-Id"),
			r.Header.Get("User-Agent"),
		)
	}))
	defer srv.Close()

	// =========================================================================
	// 1. WithTransportMiddleware — chaining multiple RoundTripper wrappers.
	//
	// Order of wrapping: timing → requestID → relay internals → HTTP.
	// Order of interception (outermost first): requestID → timing → HTTP.
	// =========================================================================
	fmt.Println("=== Transport middleware chain ===")

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDefaultHeaders(map[string]string{"User-Agent": "relay-middleware-demo/1.0"}),

		// Added first → applied innermost (closer to the wire).
		relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
			return &timingMiddleware{base: next}
		}),

		// Added second → applied outermost (first to intercept).
		relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
			return &requestIDMiddleware{base: next}
		}),
	)

	resp, err := client.Execute(client.Get("/api/users"))
	if err != nil {
		log.Fatalf("request failed: %v", err)
	}
	fmt.Printf("  body: %s\n\n", resp.String())

	resp, err = client.Execute(client.Post("/api/orders").WithJSON(map[string]any{"qty": 3}))
	if err != nil {
		log.Fatalf("POST failed: %v", err)
	}
	fmt.Printf("  body: %s\n\n", resp.String())

	// =========================================================================
	// 2. WithOnBeforeRequest — hook fired before each attempt (incl. retries).
	//
	// Use for: dynamic token injection, request stamping, distributed tracing
	// context propagation, per-attempt logging.
	// =========================================================================
	fmt.Println("=== OnBeforeRequest hooks ===")

	attempt := 0
	hookedClient := relay.New(
		relay.WithBaseURL(srv.URL),

		// Hook 1: stamp a dynamic timestamp on every attempt.
		relay.WithOnBeforeRequest(func(ctx context.Context, req *relay.Request) error {
			req.WithHeader("X-Request-Time", time.Now().UTC().Format(time.RFC3339Nano))
			return nil
		}),

		// Hook 2: count attempts and log.
		relay.WithOnBeforeRequest(func(ctx context.Context, req *relay.Request) error {
			attempt++
			fmt.Printf("  [before #%d] %s %s  op=%s\n",
				attempt, req.Method(), req.URL(), req.Tag("op"))
			return nil
		}),

		// Hook 3: block requests tagged as "maintenance".
		relay.WithOnBeforeRequest(func(ctx context.Context, req *relay.Request) error {
			if req.Tag("mode") == "maintenance" {
				return fmt.Errorf("request blocked: client is in maintenance mode")
			}
			return nil
		}),
	)

	r, err := hookedClient.Execute(
		hookedClient.Get("/api/status").
			WithTag("op", "HealthCheck").
			WithTag("mode", "normal"),
	)
	if err != nil {
		log.Fatalf("unexpected error: %v", err)
	}
	fmt.Printf("  status: %d\n\n", r.StatusCode)

	// Trigger the maintenance block.
	_, err = hookedClient.Execute(
		hookedClient.Get("/api/status").WithTag("mode", "maintenance"),
	)
	if err != nil {
		fmt.Printf("  expected block: %v\n\n", err)
	}

	// =========================================================================
	// 3. WithOnAfterResponse — hook fired after every successful response.
	//
	// Use for: response validation, metrics recording, structured audit logging,
	// automatic error promotion (convert 4xx/5xx to errors).
	// =========================================================================
	fmt.Println("=== OnAfterResponse hooks ===")

	validatedClient := relay.New(
		relay.WithBaseURL(srv.URL),

		// Hook 1: log every response with timing info.
		relay.WithOnAfterResponse(func(ctx context.Context, resp *relay.Response) error {
			fmt.Printf("  [after] %d %s  bytes=%d\n",
				resp.StatusCode, resp.Status, len(resp.Body()))
			return nil
		}),

		// Hook 2: promote HTTP errors to Go errors.
		relay.WithOnAfterResponse(func(ctx context.Context, resp *relay.Response) error {
			if resp.IsServerError() {
				return fmt.Errorf("server error %d: %s", resp.StatusCode, resp.String())
			}
			return nil
		}),

		// Hook 3: enforce required response headers.
		relay.WithOnAfterResponse(func(ctx context.Context, resp *relay.Response) error {
			if resp.Header("Content-Type") == "" {
				return fmt.Errorf("response missing Content-Type header")
			}
			return nil
		}),
	)

	resp, err = validatedClient.Execute(validatedClient.Get("/data"))
	if err != nil {
		log.Fatalf("unexpected error: %v", err)
	}
	fmt.Printf("  body: %s\n\n", resp.String())

	// =========================================================================
	// 4. Middleware ordering: last-added is outermost (first to intercept).
	// =========================================================================
	fmt.Println("=== Middleware ordering (last-added = outermost) ===")

	var order []string
	orderedClient := relay.New(
		relay.WithBaseURL(srv.URL),

		// Added first → innermost (closest to actual HTTP call).
		relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
			return roundTripFunc(func(req *http.Request) (*http.Response, error) {
				order = append(order, "middleware-A enter")
				resp, err := next.RoundTrip(req)
				order = append(order, "middleware-A exit")
				return resp, err
			})
		}),

		// Added second → outermost (first to intercept).
		relay.WithTransportMiddleware(func(next http.RoundTripper) http.RoundTripper {
			return roundTripFunc(func(req *http.Request) (*http.Response, error) {
				order = append(order, "middleware-B enter")
				resp, err := next.RoundTrip(req)
				order = append(order, "middleware-B exit")
				return resp, err
			})
		}),
	)

	orderedClient.Execute(orderedClient.Get("/order")) //nolint:errcheck
	for _, step := range order {
		fmt.Printf("  %s\n", step)
	}
	fmt.Println("  (B wraps A: B enters first, A is closer to the wire)")

	// =========================================================================
	// 5. Combining middleware + hooks with Client.With for per-operation clients.
	// =========================================================================
	fmt.Println("\n=== Per-operation client variant with Client.With ===")

	base := relay.New(
		relay.WithBaseURL(srv.URL),
		relay.WithDefaultHeaders(map[string]string{"X-Service": "inventory"}),
	)

	// Derive a variant that adds auth + audit for sensitive endpoints.
	audited := base.With(
		relay.WithOnBeforeRequest(func(_ context.Context, req *relay.Request) error {
			req.WithHeader("X-Audit-User", "system")
			return nil
		}),
		relay.WithOnAfterResponse(func(_ context.Context, resp *relay.Response) error {
			fmt.Printf("  [audit] %d %s\n", resp.StatusCode, resp.Status)
			return nil
		}),
	)

	audited.Execute(audited.Get("/api/sensitive")) //nolint:errcheck
}

// roundTripFunc is an adapter that lets a plain function implement http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
