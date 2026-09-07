package memory

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/testutil"
)

// These benchmarks pair relay against a bare net/http.Client hitting the same
// local httptest server, so -benchmem numbers are directly comparable. They
// exist to track relay's per-request overhead over net/http as a regression
// gate, not to prove relay is always cheaper - see CHANGELOG "Unreleased" for
// the current known gap and the plan to close it.

var smallJSON = []byte(`{"id":1,"name":"test","value":42}`)

func BenchmarkMemory_VsNetHTTP_Relay_GET(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: string(smallJSON)})
		resp, err := client.Execute(client.Get("/native/small"))
		if err != nil {
			b.Fatal(err)
		}
		_ = resp
	}
}

func BenchmarkMemory_VsNetHTTP_Native_GET(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	httpClient := &http.Client{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: string(smallJSON)})

		req, err := http.NewRequest(http.MethodGet, srv.URL()+"/native/small", nil)
		if err != nil {
			b.Fatal(err)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close() //nolint:errcheck
		_ = body
	}
}

// BenchmarkMemory_VsNetHTTP_Relay_GET_WithTiming shows the cost of opting
// into per-request httptrace timing (WithTiming; off by default). Per the
// pprof investigation behind this benchmark, httptrace.WithClientTrace
// (only injected on Execute when timing is enabled) is relay's single
// largest self-owned allocation site - larger than any other relay-specific
// hot-path cost. Compare against BenchmarkMemory_VsNetHTTP_Relay_GET, which
// is now the zero-overhead default, to see the cost of the opt-in timing
// feature in isolation.
func BenchmarkMemory_VsNetHTTP_Relay_GET_WithTiming(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTiming(),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: string(smallJSON)})
		resp, err := client.Execute(client.Get("/native/small"))
		if err != nil {
			b.Fatal(err)
		}
		_ = resp
	}
}

// BenchmarkMemory_VsNetHTTP_Relay_GET_NoRedirectTracking shows the
// allocation floor with redirect tracking disabled (WithDisableRedirectTracking;
// on by default). Compare against BenchmarkMemory_VsNetHTTP_Relay_GET to see
// the cost of the redirectState pool round-trip + context.WithValue in
// isolation.
func BenchmarkMemory_VsNetHTTP_Relay_GET_NoRedirectTracking(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithDisableRedirectTracking(),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: string(smallJSON)})
		resp, err := client.Execute(client.Get("/native/small"))
		if err != nil {
			b.Fatal(err)
		}
		_ = resp
	}
}

func BenchmarkMemory_VsNetHTTP_Relay_POST(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: string(smallJSON)})
		resp, err := client.Execute(client.Post("/native/small").WithBody(smallJSON).WithHeader("Content-Type", "application/json"))
		if err != nil {
			b.Fatal(err)
		}
		_ = resp
	}
}

func BenchmarkMemory_VsNetHTTP_Native_POST(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	httpClient := &http.Client{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: string(smallJSON)})

		req, err := http.NewRequest(http.MethodPost, srv.URL()+"/native/small", bytes.NewReader(smallJSON))
		if err != nil {
			b.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close() //nolint:errcheck
		_ = body
	}
}

// --- Redirect-heavy: 3-hop chain before the final 200 -----------------------

func BenchmarkMemory_VsNetHTTP_Relay_Redirect3Hop(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(
			testutil.MockResponse{Status: http.StatusFound, Headers: map[string]string{"Location": "/hop1"}},
			testutil.MockResponse{Status: http.StatusFound, Headers: map[string]string{"Location": "/hop2"}},
			testutil.MockResponse{Status: http.StatusFound, Headers: map[string]string{"Location": "/hop3"}},
			testutil.MockResponse{Status: http.StatusOK, Body: string(smallJSON)},
		)
		resp, err := client.Execute(client.Get("/redirect/start"))
		if err != nil {
			b.Fatal(err)
		}
		_ = resp
	}
}

func BenchmarkMemory_VsNetHTTP_Native_Redirect3Hop(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	httpClient := &http.Client{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(
			testutil.MockResponse{Status: http.StatusFound, Headers: map[string]string{"Location": "/hop1"}},
			testutil.MockResponse{Status: http.StatusFound, Headers: map[string]string{"Location": "/hop2"}},
			testutil.MockResponse{Status: http.StatusFound, Headers: map[string]string{"Location": "/hop3"}},
			testutil.MockResponse{Status: http.StatusOK, Body: string(smallJSON)},
		)

		req, err := http.NewRequest(http.MethodGet, srv.URL()+"/redirect/start", nil)
		if err != nil {
			b.Fatal(err)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close() //nolint:errcheck
		_ = body
	}
}

// --- Header-heavy: 40 request headers, several multi-word values -----------

var manyHeaders = func() map[string]string {
	h := make(map[string]string, 40)
	for i := 0; i < 40; i++ {
		h[fmt.Sprintf("X-Bench-Header-%02d", i)] = fmt.Sprintf("value-%02d-abcdefghijklmnopqrstuvwxyz", i)
	}
	return h
}()

func BenchmarkMemory_VsNetHTTP_Relay_ManyHeaders(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	client := relay.New(
		relay.WithBaseURL(srv.URL()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
	)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: string(smallJSON)})
		resp, err := client.Execute(client.Get("/many-headers").WithHeaders(manyHeaders))
		if err != nil {
			b.Fatal(err)
		}
		_ = resp
	}
}

func BenchmarkMemory_VsNetHTTP_Native_ManyHeaders(b *testing.B) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	httpClient := &http.Client{}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK, Body: string(smallJSON)})

		req, err := http.NewRequest(http.MethodGet, srv.URL()+"/many-headers", nil)
		if err != nil {
			b.Fatal(err)
		}
		for k, v := range manyHeaders {
			req.Header.Set(k, v)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			b.Fatal(err)
		}
		resp.Body.Close() //nolint:errcheck
		_ = body
	}
}
