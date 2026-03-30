// Package relay provides a production-grade, declarative HTTP client for Go.
//
// relay is designed with the ergonomics of Python's requests, Kotlin's OkHttp,
// and Java's OpenFeign — batteries included.
//
// Quick start:
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relay.WithTimeout(10 * time.Second),
//	)
//	resp, err := client.Execute(client.Get("/users/42"))
//
// See https://github.com/jhonsferg/relay for full documentation.
package relay
