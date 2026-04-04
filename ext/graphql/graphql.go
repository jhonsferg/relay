// Package graphql provides a GraphQL client built on top of relay.Client.
// It handles the GraphQL HTTP transport protocol (query/mutation execution,
// variable passing, error extraction) while delegating retry, circuit breaking,
// rate limiting, and authentication to the underlying relay.Client.
//
// # Basic usage
//
//	client := relay.New(relay.WithBaseURL("https://api.example.com"))
//	gql := graphql.New(client, "/graphql")
//
//	type User struct {
//	    ID   string `json:"id"`
//	    Name string `json:"name"`
//	}
//	type GetUserData struct {
//	    User User `json:"user"`
//	}
//
//	query := `query GetUser($id: ID!) { user(id: $id) { id name } }`
//	data, err := graphql.Execute[GetUserData](gql, context.Background(), graphql.Request{
//	    Query:     query,
//	    Variables: map[string]any{"id": "123"},
//	})
//
// # Error handling
//
// When the server returns a GraphQL errors array, Execute returns an [Errors]
// value which implements error. Partial data (data + errors) is not returned;
// callers should check err first.
package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jhonsferg/relay"
)

// Client is a GraphQL client. Create one with [New].
type Client struct {
	relay    *relay.Client
	endpoint string
}

// New creates a GraphQL client that sends requests to endpoint using the given
// relay.Client for transport. endpoint may be a relative path (e.g. "/graphql")
// when the relay.Client has a base URL configured, or a fully-qualified URL.
func New(client *relay.Client, endpoint string) *Client {
	return &Client{relay: client, endpoint: endpoint}
}

// Request is a GraphQL request document.
type Request struct {
	// Query is the GraphQL query or mutation document.
	Query string `json:"query"`

	// Variables are the input variables for the operation.
	Variables map[string]any `json:"variables,omitempty"`

	// OperationName selects a named operation from a multi-operation document.
	// Leave empty when the document contains exactly one operation.
	OperationName string `json:"operationName,omitempty"`
}

// envelope is the raw GraphQL HTTP response.
type envelope[T any] struct {
	Data   *T     `json:"data"`
	Errors Errors `json:"errors,omitempty"`
}

// Execute sends a GraphQL query or mutation and deserialises the response data
// section into T. If the server returns a non-empty errors array, Execute
// returns a zero T and an [Errors] error. Partial responses (data + errors)
// are treated as errors.
//
// All relay.Client features (retry, circuit breaker, auth via WithSigner,
// rate limiting, tracing) are applied transparently.
func Execute[T any](c *Client, ctx context.Context, req Request) (T, error) {
	var zero T

	body, err := json.Marshal(req)
	if err != nil {
		return zero, fmt.Errorf("graphql: marshal request: %w", err)
	}

	httpReq := c.relay.Post(c.endpoint).
		WithContext(ctx).
		WithBody(body).
		WithHeader("Content-Type", "application/json")

	resp, err := c.relay.Execute(httpReq)
	if err != nil {
		return zero, err
	}
	defer relay.PutResponse(resp)

	var env envelope[T]
	if jsonErr := resp.JSON(&env); jsonErr != nil {
		return zero, fmt.Errorf("graphql: decode response: %w", jsonErr)
	}

	if len(env.Errors) > 0 {
		return zero, env.Errors
	}

	if env.Data == nil {
		return zero, fmt.Errorf("graphql: response data is null")
	}

	return *env.Data, nil
}

// Error is a single GraphQL error as defined in the GraphQL specification.
type Error struct {
	// Message is a human-readable description of the error.
	Message string `json:"message"`

	// Locations identifies the location(s) in the query document related to
	// the error.
	Locations []Location `json:"locations,omitempty"`

	// Path is the path in the response data where the error occurred.
	Path []any `json:"path,omitempty"`

	// Extensions contains additional error metadata supplied by the server.
	Extensions map[string]any `json:"extensions,omitempty"`
}

// Error implements the error interface.
func (e Error) Error() string { return e.Message }

// Errors is a slice of [Error] values returned by the server.
// It implements the error interface so callers can treat a GraphQL error
// response like any other Go error.
type Errors []Error

// Error returns a single string combining all error messages.
func (e Errors) Error() string {
	if len(e) == 1 {
		return e[0].Message
	}
	msgs := make([]string, len(e))
	for i, err := range e {
		msgs[i] = err.Message
	}
	return "graphql: " + strings.Join(msgs, "; ")
}

// Location is a line/column position within a GraphQL document.
type Location struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}
