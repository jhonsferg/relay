package sigv4_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials"

	"github.com/jhonsferg/relay"
	relaysigv4 "github.com/jhonsferg/relay/ext/sigv4"
)

// BenchmarkSigV4_RoundTrip measures the per-request overhead of the sigv4
// transport middleware: retrieving credentials, hashing the request body,
// and computing the SigV4 signature (Authorization/X-Amz-Date headers) end
// to end through a relay.Client.
func BenchmarkSigV4_RoundTrip(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	creds := credentials.NewStaticCredentialsProvider("AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "")

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relaysigv4.WithSigV4(&creds, "execute-api", "us-east-1"),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(5*time.Second),
	)

	payload := bytes.Repeat([]byte(`{"id":1,"name":"relay-benchmark-payload"}`), 10)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Post("/resource").WithBody(payload))
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("unexpected status %d", resp.StatusCode)
		}
		relay.PutResponse(resp)
	}
}

// BenchmarkSigV4_RoundTrip_UnsignedPayload measures the same path with
// WithUnsignedPayload, which skips reading/hashing the body.
func BenchmarkSigV4_RoundTrip_UnsignedPayload(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	creds := credentials.NewStaticCredentialsProvider("AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "")

	client := relay.New(
		relay.WithBaseURL(srv.URL),
		relaysigv4.WithSigV4(&creds, "execute-api", "us-east-1", relaysigv4.WithUnsignedPayload()),
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithTimeout(5*time.Second),
	)

	payload := bytes.Repeat([]byte(`{"id":1,"name":"relay-benchmark-payload"}`), 10)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		resp, err := client.Execute(client.Post("/resource").WithBody(payload))
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			b.Fatalf("unexpected status %d", resp.StatusCode)
		}
		relay.PutResponse(resp)
	}
}
