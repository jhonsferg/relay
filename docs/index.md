---
hide:
  - navigation
---

# relay

**A production-grade declarative HTTP client for Go.**

[![Go Version](https://img.shields.io/badge/Go-1.24%2B-00ADD8?style=for-the-badge&logo=go)](https://pkg.go.dev/github.com/jhonsferg/relay)
[![CI](https://img.shields.io/github/actions/workflow/status/jhonsferg/relay/ci.yml?style=for-the-badge&logo=github&label=CI)](https://github.com/jhonsferg/relay/actions)
[![Coverage](https://img.shields.io/codecov/c/github/jhonsferg/relay?style=for-the-badge&logo=codecov)](https://codecov.io/gh/jhonsferg/relay)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg?style=for-the-badge)](https://github.com/jhonsferg/relay/blob/master/LICENSE)
[![pkg.go.dev](https://img.shields.io/badge/pkg.go.dev-reference-007D9C?style=for-the-badge&logo=go)](https://pkg.go.dev/github.com/jhonsferg/relay)

relay is a zero-boilerplate HTTP client that handles the hard parts - retries, circuit breaking, rate limiting, authentication, observability - so you can focus on your application logic.

## Why relay?

| Feature | relay | net/http | resty | heimdall |
|---------|-------|----------|-------|----------|
| Retry with backoff | Yes | No | Yes | Yes |
| Circuit breaker | Yes | No | No | Yes |
| Bulkhead isolation | Yes | No | No | No |
| Request hedging | Yes | No | No | No |
| Semantic hooks | Yes | No | Partial | No |
| WebSocket (same client) | Yes | No | No | No |
| Dynamic TLS cert reload | Yes | No | No | No |
| Transport adapters | Yes | No | No | No |
| Extension ecosystem | 18 modules | - | few | - |

## Quick install

```bash
go get github.com/jhonsferg/relay
```

## 30-second example

```go
package main

import (
    "context"
    "fmt"
    "github.com/jhonsferg/relay"
)

type User struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

func main() {
    client := relay.New(
        relay.WithBaseURL("https://api.example.com"),
        relay.WithBearerToken("my-token"),
        relay.WithRetry(&relay.RetryConfig{MaxAttempts: 3}),
    )

    req := client.Get("/users/1")
    resp, err := client.Execute(context.Background(), req)
    if err != nil {
        panic(err)
    }

    user, err := relay.DecodeJSON[User](resp)
    if err != nil {
        panic(err)
    }
    fmt.Printf("Hello, %s!\n", user.Name)
}
```

## Features at a glance

<div class="grid cards" markdown>

- :material-repeat: **Retry & Backoff** - Exponential backoff with jitter, retry budgets, per-status configuration
- :material-security: **Auth** - Bearer, Basic, API Key, Digest, OAuth2, HMAC, AWS SigV4
- :material-shield-off: **Circuit Breaker** - Sliding-window failure detection with automatic recovery
- :material-speedometer: **Rate Limiting** - Token bucket with burst support
- :material-hook: **Hooks** - BeforeRetry, BeforeRedirect, OnError semantic hooks
- :material-scale-balance: **Bulkhead** - Limit concurrent requests to protect downstream services
- :material-fast-forward: **Hedging** - Race duplicate requests for tail-latency reduction
- :material-web: **WebSocket** - First-class WebSocket upgrade using the same client config
- :material-certificate: **TLS** - Dynamic certificate reloading without restarts
- :material-swap-horizontal: **Transport** - Custom transports per URL scheme, HTTP/3 QUIC
- :material-puzzle: **Extensions** - 18 optional modules: OTel, Prometheus, Sentry, gRPC, GraphQL...
- :material-test-tube: **Testing** - Built-in mock server and VCR cassette recording

</div>
