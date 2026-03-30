// Package main demonstrates the fundamental usage patterns of the relay HTTP
// client: creating a client, making GET and POST requests, inspecting
// responses, and using response helper methods.
package main

import (
	"fmt"
	"log"
	"time"

	relay "github.com/jhonsferg/relay"
)

func main() {
	// ---------------------------------------------------------------------------
	// 1. Create a client
	//
	// relay.New accepts functional options. Here we set a base URL so that
	// relative paths like "/posts" are resolved automatically, a global
	// request timeout, and a default Accept header sent on every request.
	// ---------------------------------------------------------------------------
	client := relay.New(
		relay.WithBaseURL("https://jsonplaceholder.typicode.com"),
		relay.WithTimeout(15*time.Second),
		relay.WithDefaultHeaders(map[string]string{
			"Accept": "application/json",
		}),
	)

	// ---------------------------------------------------------------------------
	// 2. GET request
	//
	// client.Get returns a *relay.Request builder. Chain With* methods to
	// customise it, then pass it to client.Execute (or ExecuteJSON / ExecuteAs).
	// ---------------------------------------------------------------------------
	resp, err := client.Execute(
		client.Get("/posts/1").
			WithQueryParam("_embed", "comments"), // adds ?_embed=comments
	)
	if err != nil {
		log.Fatalf("GET /posts/1 failed: %v", err)
	}

	// Execute never returns an error for HTTP 4xx/5xx — check IsError() instead.
	if resp.IsError() {
		log.Fatalf("server returned error status: %s — body: %s", resp.Status, resp.String())
	}

	fmt.Printf("GET /posts/1 → %d %s\n", resp.StatusCode, resp.Status)
	fmt.Printf("  Content-Type : %s\n", resp.ContentType())
	fmt.Printf("  Body preview : %.80s…\n\n", resp.String())

	// ---------------------------------------------------------------------------
	// 3. deserialize the response body into a typed struct
	//
	// ExecuteJSON calls Execute and, on a 2xx response, unmarshals the JSON
	// body into the provided pointer.
	// ---------------------------------------------------------------------------
	type Post struct {
		ID     int    `json:"id"`
		UserID int    `json:"userId"`
		Title  string `json:"title"`
		Body   string `json:"body"`
	}

	var post Post
	resp, err = client.ExecuteJSON(client.Get("/posts/2"), &post)
	if err != nil {
		log.Fatalf("ExecuteJSON /posts/2 failed: %v", err)
	}
	fmt.Printf("ExecuteJSON /posts/2 → id=%d title=%q\n\n", post.ID, post.Title)

	// ExecuteAs is the generic alternative — no explicit target variable needed.
	post2, resp2, err := relay.ExecuteAs[Post](client, client.Get("/posts/3"))
	if err != nil {
		log.Fatalf("ExecuteAs /posts/3 failed: %v", err)
	}
	_ = resp2 // resp2 is available if you need headers / status code
	fmt.Printf("ExecuteAs   /posts/3 → id=%d title=%q\n\n", post2.ID, post2.Title)

	// ---------------------------------------------------------------------------
	// 4. POST with a JSON body
	//
	// WithJSON marshals the value, sets the body, and sets Content-Type to
	// application/json automatically.
	// ---------------------------------------------------------------------------
	newPost := Post{
		UserID: 1,
		Title:  "relay is great",
		Body:   "fast, safe, and ergonomic",
	}

	var created Post
	resp, err = client.ExecuteJSON(
		client.Post("/posts").
			WithJSON(newPost).
			WithHeader("X-Request-Source", "relay-example"),
		&created,
	)
	if err != nil {
		log.Fatalf("POST /posts failed: %v", err)
	}
	if !resp.IsSuccess() {
		log.Fatalf("unexpected status %s", resp.Status)
	}
	fmt.Printf("POST /posts → created id=%d\n\n", created.ID)

	// ---------------------------------------------------------------------------
	// 5. Response helpers
	// ---------------------------------------------------------------------------
	fmt.Println("Response helper summary:")
	fmt.Printf("  IsSuccess()     = %v\n", resp.IsSuccess())
	fmt.Printf("  IsError()       = %v\n", resp.IsError())
	fmt.Printf("  IsClientError() = %v\n", resp.IsClientError())
	fmt.Printf("  IsServerError() = %v\n", resp.IsServerError())
	fmt.Printf("  WasRedirected() = %v\n", resp.WasRedirected())
	fmt.Printf("  IsTruncated()   = %v\n", resp.IsTruncated())

	// AsHTTPError returns nil for 2xx responses; non-nil for 4xx/5xx.
	if httpErr := resp.AsHTTPError(); httpErr != nil {
		fmt.Printf("  HTTPError: %v\n", httpErr)
	}

	// Body() returns the raw bytes; String() decodes them as UTF-8.
	fmt.Printf("  Body length     = %d bytes\n", len(resp.Body()))
}
