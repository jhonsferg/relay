package graphql_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/ext/graphql"
)

// BenchmarkExecute_Success measures the per-request overhead of
// graphql.Execute on a successful response: marshalling the request,
// round-tripping through relay.Client, and decoding the envelope.
func BenchmarkExecute_Success(b *testing.B) {
	const responseBody = `{"data":{"user":{"id":"1","name":"Alice"}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseBody))
	}))
	defer srv.Close()

	type User struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type Data struct {
		User User `json:"user"`
	}

	client := graphql.New(relay.New(relay.WithDisableCircuitBreaker(), relay.WithDisableRetry()), srv.URL)
	req := graphql.Request{
		Query:     `query { user { id name } }`,
		Variables: map[string]any{"id": "1"},
	}
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		data, err := graphql.Execute[Data](client, ctx, req)
		if err != nil {
			b.Fatalf("Execute: %v", err)
		}
		if data.User.ID != "1" {
			b.Fatalf("unexpected data: %+v", data)
		}
	}
}
