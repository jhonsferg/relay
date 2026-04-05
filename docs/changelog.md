# Changelog

All notable changes to relay are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
relay uses [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [0.3.0] - Phase G: Protocol Extensions

### Added

- **WebSocket support** (`websocket.go`) - full-duplex connections with typed message handlers, ping/pong keepalive, and automatic reconnection
- **Dynamic TLS watcher** (`tls_watcher.go`) - hot-reload TLS certificates from disk without restarting the client; watches for file changes and rotates credentials transparently
- **Transport adapters** (`transport_adapters.go`) - plug in custom `http.RoundTripper` implementations; includes built-in adapters for Unix socket, in-memory, and custom dial functions
- **HTTP/3 QUIC extension** (`ext/http3/`) - QUIC transport via `quic-go`; automatic fallback to HTTP/2 when QUIC is unavailable

---

## [0.2.0] - Phase F: Resilience & Advanced Features

### Added

- **Bulkhead isolation** - per-endpoint concurrency limits using semaphores to prevent one slow upstream from consuming all goroutines
- **Request hedging** (`hedging.go`) - send a duplicate request after a configurable delay; the fastest response wins, improving tail latency
- **Content negotiation** - automatic `Accept` and `Content-Type` header management with codec registration (JSON, CBOR, MessagePack, XML)
- **Timeout tiers** (`config.go`) - separate dial, TLS handshake, response header, and total request timeouts
- **Typed pagination** (`pagination.go`) - generic `Paginator[T]` that follows `Link`, cursor, offset, or custom next-page strategies
- **Circuit breaker** - token-bucket state machine that opens after N failures and probes recovery after a configurable timeout
- **DNS cache** - in-process DNS resolution cache with configurable TTL to reduce DNS lookup latency

---

## [0.1.0] - Phase E: Developer Experience & Foundations

### Added

- **Semantic hooks** - typed lifecycle hooks: `OnRequest`, `OnResponse`, `OnError`, `OnRetry`
- **Idempotency keys** (`idempotency.go`) - automatic `Idempotency-Key` header injection with configurable generators
- **Response classification** (`classification.go`) - categorise responses into success, client error, server error, redirect with structured error types
- **Generic decode helpers** (`generics.go`) - `relay.Decode[T]()` and `relay.DecodeSlice[T]()` for zero-boilerplate typed response parsing
- **Redirect chain** (`response.go`) - `Response.RedirectChain()` exposes the full list of intermediate URLs followed during a redirect

---

## [0.0.1] - Initial release

### Added

- Declarative request builder with method chaining
- Automatic JSON marshal/unmarshal for request bodies and responses
- Configurable retry with exponential backoff, jitter, and per-status-code policies
- `BaseURL` + path composition
- Context propagation and cancellation support
- Structured error types with status code and body access
- Default headers and query parameters
- Basic authentication and Bearer token helpers
- `http.RoundTripper` transport slot for full customisation
