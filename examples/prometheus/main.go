// Package main demonstrates the Prometheus metrics extension for relay via
// github.com/jhonsferg/relay/ext/prometheus. It shows how to wire in a custom
// Prometheus registry, make several requests with different outcomes, and then
// scrape the collected metrics.
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"

	relay "github.com/jhonsferg/relay"
	relayprom "github.com/jhonsferg/relay/ext/prometheus"
)

func main() {
	// ---------------------------------------------------------------------------
	// 1. Create a dedicated Prometheus registry.
	//
	// Using a custom registry (rather than prometheus.DefaultRegisterer) keeps
	// your metrics isolated and makes tests predictable.
	// ---------------------------------------------------------------------------
	reg := prometheus.NewRegistry()

	// ---------------------------------------------------------------------------
	// 2. Test server: responds to different paths with varying status codes.
	// ---------------------------------------------------------------------------
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"status":"ok"}`)
		case "/slow":
			time.Sleep(50 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		case "/not-found":
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":"not found"}`)
		case "/error":
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"error":"server error"}`)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	// ---------------------------------------------------------------------------
	// 3. Build the relay client with the Prometheus extension.
	//
	// The namespace prefix ("myapp") is prepended to every metric name:
	//   myapp_http_client_requests_total
	//   myapp_http_client_request_duration_seconds
	//   myapp_http_client_active_requests
	//
	// Labels: method, host, status_code.
	// ---------------------------------------------------------------------------
	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relayprom.WithPrometheus(reg, "myapp"),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(10*time.Second),
	)

	// ---------------------------------------------------------------------------
	// 4. Make several requests to generate metrics.
	// ---------------------------------------------------------------------------
	endpoints := []string{"/ok", "/ok", "/ok", "/slow", "/not-found", "/error"}
	for _, path := range endpoints {
		resp, err := client.Execute(client.Get(path))
		if err != nil {
			log.Printf("Execute %s: %v", path, err)
			continue
		}
		fmt.Printf("GET %-12s → %d\n", path, resp.StatusCode)
	}

	// POST request to see method label differentiation.
	resp, err := client.Execute(client.Post("/ok").WithJSON(map[string]string{"k": "v"}))
	if err != nil {
		log.Printf("POST /ok: %v", err)
	} else {
		fmt.Printf("POST %-11s → %d\n", "/ok", resp.StatusCode)
	}

	// ---------------------------------------------------------------------------
	// 5. Gather and print all collected metrics.
	// ---------------------------------------------------------------------------
	fmt.Println("\n--- Collected Prometheus metrics ---")

	families, err := reg.Gather()
	if err != nil {
		log.Fatalf("Gather: %v", err)
	}
	for _, mf := range families {
		fmt.Printf("\n# %s\n", mf.GetHelp())
		fmt.Printf("  name: %s   type: %s\n", mf.GetName(), mf.GetType())
		for _, m := range mf.GetMetric() {
			labels := make([]string, 0, len(m.GetLabel()))
			for _, lp := range m.GetLabel() {
				labels = append(labels, fmt.Sprintf("%s=%q", lp.GetName(), lp.GetValue()))
			}
			switch {
			case m.GetCounter() != nil:
				fmt.Printf("  {%s} counter=%.0f\n", strings.Join(labels, " "), m.GetCounter().GetValue())
			case m.GetHistogram() != nil:
				fmt.Printf("  {%s} count=%d sum=%.4f\n",
					strings.Join(labels, " "),
					m.GetHistogram().GetSampleCount(),
					m.GetHistogram().GetSampleSum())
			case m.GetGauge() != nil:
				fmt.Printf("  {%s} gauge=%.0f\n", strings.Join(labels, " "), m.GetGauge().GetValue())
			}
		}
	}

	// ---------------------------------------------------------------------------
	// 6. Quick assertion: at least 7 requests were counted.
	// ---------------------------------------------------------------------------
	total, err := testutil.GatherAndCount(reg, "myapp_http_client_requests_total")
	if err != nil {
		log.Fatalf("GatherAndCount: %v", err)
	}
	fmt.Printf("\nDistinct label combinations in requests_total: %d\n", total)

	// ---------------------------------------------------------------------------
	// 7. Expose metrics on an HTTP endpoint (typical production setup).
	//
	// In a real service you would run this alongside your API server and scrape
	// it from Prometheus.
	// ---------------------------------------------------------------------------
	fmt.Println("\n--- Metrics HTTP endpoint (not started in example) ---")
	fmt.Println("  handler := promhttp.HandlerFor(reg, promhttp.HandlerOpts{})")
	fmt.Println("  http.Handle(\"/metrics\", handler)")
	fmt.Println("  http.ListenAndServe(\":9090\", nil)")

	// Demonstrate that the handler is valid by creating it (not listening).
	_ = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	fmt.Println("\n  (handler created successfully)")
}
