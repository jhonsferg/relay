// Package relay provides a production-grade, declarative HTTP client for Go.
//
// relay is designed with the ergonomics of Python's requests, Kotlin's OkHttp,
// and Java's OpenFeign - batteries included.
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
//
// # WASM / js target
//
// The core relay package compiles and runs under GOOS=js GOARCH=wasm using
// Go's Fetch API backend. Features that rely on OS-level networking
// (e.g. [WithUnixSocket]) are silently ignored on that target. Extension
// modules under ext/ may have additional native dependencies and are not
// guaranteed to be WASM-compatible.
package relay
