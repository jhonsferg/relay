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
// # API Stability
//
// relay follows [Semantic Versioning]. The following rules apply to the public
// API defined in this package and all ext/* modules:
//
//   - All exported types, functions, and methods listed under "Stable API"
//     below will not have backward-incompatible changes before v1.0.
//   - Types and functions listed under "Experimental API" may change between
//     minor releases. They are guarded by godoc comments that say "experimental".
//   - The internal/ package is not part of the public API and may change at
//     any time.
//
// ## Stable API (guaranteed no breaking changes before v1.0)
//
// Core client:
//   - [New], [Client], [Config], [Option]
//   - [Client.Execute], [Client.Get], [Client.Post], [Client.Put],
//     [Client.Patch], [Client.Delete], [Client.Head], [Client.Options]
//   - [Client.ExecuteAsync], [Client.ExecuteBatch], [Client.ExecuteStream]
//   - [Client.With], [Client.Shutdown], [Client.BaseURL]
//   - [Request] builder — all chaining methods (.Header, .Query, .Body, .Path, …)
//   - [Response] — Status, StatusCode, Body, Headers, Timing
//
// Error handling:
//   - [HTTPError], [IsHTTPError], [ErrorClass], [ClassifyError]
//   - [IsTransientError], [IsPermanentError], [IsRateLimitedError],
//     [IsRetryableError], [IsTimeout], [IsCircuitOpen]
//   - [ErrCertificatePinMismatch], [ErrRetryBudgetExhausted]
//
// Resilience options:
//   - [WithRetry], [RetryConfig], [WithDisableRetry]
//   - [WithCircuitBreaker], [CircuitBreakerConfig], [CircuitBreaker],
//     [CircuitBreakerState], [WithDisableCircuitBreaker]
//   - [WithRateLimit], [RateLimitConfig]
//   - [WithLoadBalancer], [LoadBalancerConfig], [LoadBalancerStrategy]
//   - [WithTimeout], [WithAdaptiveTimeout], [AdaptiveTimeoutConfig]
//   - [WithBulkhead], [WithHedging], [WithHedgingN]
//   - [WithRetryBudget], [RetryBudget], [ErrRetryBudgetExhausted]
//
// Auth:
//   - [WithBasicAuth], [BasicAuthCreds]
//   - [WithBearerToken]
//   - [WithDigestAuth]
//   - [WithRequestSigner], [RequestSigner], [RequestSignerFunc]
//   - [WithSigner], [HMACRequestSigner], [MultiSigner], [NewMultiSigner]
//
// Transport:
//   - [WithBaseURL], [WithConnectionPool], [WithTimeout]
//   - [WithTLSConfig], [WithCertificatePinning], [WithClientCert],
//     [WithClientCertPEM], [WithDynamicTLSCert], [WithCertWatcher]
//   - [WithProxy], [WithTransportMiddleware]
//   - [WithUnixSocket]
//   - [WithCompression], [WithDisableCompression], [CompressionAlgorithm]
//   - [WithHTTP2], [WithHTTP2PushHandler], [PushPromiseHandler]
//
// Caching:
//   - [WithInMemoryCache], [WithCache], [CacheStore], [CachedResponse],
//     [NewInMemoryCacheStore]
//
// Streaming & async:
//   - [ExecuteAs], [ExecuteAsStream], [DecodeAs], [DecodeJSON], [DecodeXML]
//   - [AsyncResult], [ExecuteAsync], [ExecuteAsyncCallback]
//   - [BatchResult], [ExecuteBatch]
//   - [SSEEvent], [SSEHandler], [ExecuteSSE], [SSEFanOut], [NewSSEFanOut]
//   - [StreamResponse]
//
// Observability:
//   - [WithHARRecording], [WithHARRecorder], [HARRecorder], [NewHARRecorder]
//   - [HAR], [HAREntry], [HARLog], [HARRequest], [HARResponse]
//   - [WithLogger], [Logger], [SlogAdapter], [NewDefaultLogger], [NoopLogger]
//   - [RequestTiming]
//
// Hooks:
//   - [WithOnBeforeRequest], [WithOnAfterResponse], [WithOnErrorHook]
//   - [OnErrorHookFunc], [BeforeRetryHookFunc], [BeforeRedirectHookFunc]
//
// URL building:
//   - [WithAutoNormaliseURL], [WithURLNormalisation], [URLNormalisationMode]
//   - [NormaliseBaseURL], [NewPathBuilder], [PathBuilder]
//
// ## Experimental API (may change in minor releases)
//
// The following are functional and tested but their exact signatures or
// option shapes may be refined before v1.0:
//
//   - [WithSchemaValidation], [JSONSchemaValidator], [StructValidator],
//     [NewJSONSchemaValidator], [NewStructValidator], [SchemaValidator],
//     [ValidationError] — schema validation API is stabilising
//   - [SRVResolver], [NewSRVResolver], [SRVBalancer], [SRVOption],
//     [ResolutionResult] — SRV discovery API may gain a Watch() method
//   - [CredentialProvider], [RotatingTokenProvider], [NewRotatingTokenProvider],
//     [WithCredentialProvider], [StaticCredentialProvider] — credential
//     rotation API shape is under review
//   - [NewPushedResponseCache], [PushedResponseCache] — HTTP/2 push cache API
//   - [WSConn] — WebSocket API wrapping is experimental; consider ext/websocket
//   - [LongPollResult] — long polling API may merge with SSE in a future release
//
// # WASM / js target
//
// The core relay package compiles and runs under GOOS=js GOARCH=wasm using
// Go's Fetch API backend. Features that rely on OS-level networking
// (e.g. [WithUnixSocket]) are silently ignored on that target. Extension
// modules under ext/ may have additional native dependencies and are not
// guaranteed to be WASM-compatible.
//
// [Semantic Versioning]: https://semver.org/spec/v2.0.0.html
package relay
