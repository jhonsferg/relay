package graphql_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jhonsferg/relay"
	"github.com/jhonsferg/relay/ext/graphql"
)

// helperServer starts a test HTTP server that accepts a GraphQL POST request
// and responds with the provided JSON body.
func helperServer(t *testing.T, responseBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseBody))
	}))
}

// TestExecute_Success verifies a successful query response.
func TestExecute_Success(t *testing.T) {
	t.Parallel()

	srv := helperServer(t, `{"data":{"user":{"id":"1","name":"Alice"}}}`)
	defer srv.Close()

	type User struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type Data struct {
		User User `json:"user"`
	}

	client := graphql.New(relay.New(), srv.URL)
	data, err := graphql.Execute[Data](client, context.Background(), graphql.Request{
		Query:     `query { user { id name } }`,
		Variables: map[string]any{"id": "1"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if data.User.ID != "1" || data.User.Name != "Alice" {
		t.Errorf("data = %+v, want {User:{1 Alice}}", data)
	}
}

// TestExecute_GraphQLErrors verifies that server-side GraphQL errors are
// returned as an Errors value.
func TestExecute_GraphQLErrors(t *testing.T) {
	t.Parallel()

	srv := helperServer(t, `{"errors":[{"message":"not found"},{"message":"unauthorised"}]}`)
	defer srv.Close()

	type Data struct{ Stub string }
	client := graphql.New(relay.New(), srv.URL)
	_, err := graphql.Execute[Data](client, context.Background(), graphql.Request{
		Query: `query { stub }`,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	errs, ok := err.(graphql.Errors)
	if !ok {
		t.Fatalf("error is %T, want graphql.Errors", err)
	}
	if len(errs) != 2 {
		t.Fatalf("len(errors) = %d, want 2", len(errs))
	}
	if errs[0].Message != "not found" {
		t.Errorf("errors[0].Message = %q, want \"not found\"", errs[0].Message)
	}
}

// TestExecute_RequestBodyFormat verifies that the request body contains the
// correct GraphQL fields.
func TestExecute_RequestBodyFormat(t *testing.T) {
	t.Parallel()

	var received graphql.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer srv.Close()

	type Data struct {
		OK bool `json:"ok"`
	}
	client := graphql.New(relay.New(), srv.URL)
	_, err := graphql.Execute[Data](client, context.Background(), graphql.Request{
		Query:         `mutation DoThing($x: Int!) { doThing(x: $x) }`,
		Variables:     map[string]any{"x": 42},
		OperationName: "DoThing",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if received.Query != `mutation DoThing($x: Int!) { doThing(x: $x) }` {
		t.Errorf("received.Query = %q", received.Query)
	}
	if received.OperationName != "DoThing" {
		t.Errorf("received.OperationName = %q, want DoThing", received.OperationName)
	}
	if v, ok := received.Variables["x"]; !ok || v != float64(42) {
		t.Errorf("received.Variables[x] = %v, want 42", received.Variables["x"])
	}
}

// TestExecute_NullData verifies that a null data field returns an error.
func TestExecute_NullData(t *testing.T) {
	t.Parallel()

	srv := helperServer(t, `{"data":null}`)
	defer srv.Close()

	type Data struct{ Stub string }
	client := graphql.New(relay.New(), srv.URL)
	_, err := graphql.Execute[Data](client, context.Background(), graphql.Request{
		Query: `query { stub }`,
	})
	if err == nil {
		t.Fatal("expected error for null data, got nil")
	}
}

// TestExecute_HTTPError verifies that an HTTP-level error propagates.
func TestExecute_HTTPError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	type Data struct{ Stub string }
	// No relay-level ErrorDecoder - status 500 is not automatically an error.
	// The GraphQL envelope parse will succeed with no data/errors,
	// triggering the null data path.
	client := graphql.New(relay.New(relay.WithDisableRetry(), relay.WithDisableCircuitBreaker()), srv.URL)
	_, err := graphql.Execute[Data](client, context.Background(), graphql.Request{
		Query: `query { stub }`,
	})
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// TestErrors_Error_Single verifies the error message for a single GraphQL error.
func TestErrors_Error_Single(t *testing.T) {
	t.Parallel()
	e := graphql.Errors{{Message: "not found"}}
	if e.Error() != "not found" {
		t.Errorf("Error() = %q, want \"not found\"", e.Error())
	}
}

// TestErrors_Error_Multiple verifies the joined error message for multiple errors.
func TestErrors_Error_Multiple(t *testing.T) {
	t.Parallel()
	e := graphql.Errors{{Message: "not found"}, {Message: "unauthorised"}}
	got := e.Error()
	if got != "graphql: not found; unauthorised" {
		t.Errorf("Error() = %q", got)
	}
}

// TestExecute_UsesRelayFeatures verifies that the relay.Client's signer is
// applied to GraphQL requests (demonstrates integration with relay features).
func TestExecute_UsesRelayFeatures(t *testing.T) {
	t.Parallel()

	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer srv.Close()

	type Data struct {
		OK bool `json:"ok"`
	}
	rc := relay.New(
		relay.WithDisableRetry(),
		relay.WithDisableCircuitBreaker(),
		relay.WithSigner(relay.RequestSignerFunc(func(r *http.Request) error {
			r.Header.Set("Authorization", "Bearer test-token")
			return nil
		})),
	)
	client := graphql.New(rc, srv.URL)
	_, err := graphql.Execute[Data](client, context.Background(), graphql.Request{
		Query: `query { ok }`,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if receivedAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want \"Bearer test-token\"", receivedAuth)
	}
}
