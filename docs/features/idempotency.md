# Auto Idempotency Keys

Idempotency keys allow a client to safely retry a request that may or may not have been received and processed by the server. If a network failure occurs after the server processes the request but before the client receives the response, the client has no way to know whether the operation succeeded. Without an idempotency key, retrying a `PUT /orders/{id}` could create duplicate orders, charge a customer twice, or trigger duplicate emails.

With idempotency keys, the server can recognize a duplicate request and return the cached response from the first execution, making retry-safe operations transparent to the caller.

---

## WithAutoIdempotencyOnSafeRetries

```go
func WithAutoIdempotencyOnSafeRetries() Option
```

`WithAutoIdempotencyOnSafeRetries` enables automatic generation and injection of idempotency keys on requests that are eligible for retry. When enabled, `relay` generates a UUID v4 idempotency key before the first attempt and includes it in the `X-Idempotency-Key` header. The same key is used on all retry attempts for that logical request.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://payments.internal"),
        relay.WithAutoIdempotencyOnSafeRetries(),
        relay.WithRetry(relay.RetryConfig{
            MaxAttempts: 3,
            Backoff:     relay.ExponentialBackoff(200*time.Millisecond, 2.0),
        }),
    )
    if err != nil {
        log.Fatal(err)
    }

    // relay generates one UUID v4 key for this request and includes it
    // on every attempt: X-Idempotency-Key: 550e8400-e29b-41d4-a716-446655440000
    resp, err := client.Put(context.Background(), "/orders/ord-123/confirm", map[string]interface{}{
        "payment_method": "pm_card_visa",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("confirm status:", resp.StatusCode)
}
```

> **note**
> `WithAutoIdempotencyOnSafeRetries` only injects idempotency keys when retries are also configured. Without a retry policy, no retrying happens and injecting a key is a no-op (though it is still harmless). For best results, combine with `WithRetry`.

---

## X-Idempotency-Key Header Automatic Injection

The `X-Idempotency-Key` header is a widely adopted de facto standard, used by Stripe, Adyen, Braintree, and many other payment and operations APIs. It is also specified in the IETF draft for idempotent HTTP requests.

When `WithAutoIdempotencyOnSafeRetries` is active, `relay` handles the key lifecycle automatically:

1. Before the first attempt, a new UUID v4 is generated.
2. The UUID is set as the `X-Idempotency-Key` header.
3. On retry attempts (due to network errors or 5xx responses), the **same UUID** is reused.
4. On a completely new call to `client.Put(...)`, a fresh UUID is generated.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://subscriptions.internal"),
        relay.WithAutoIdempotencyOnSafeRetries(),
        relay.WithRetry(relay.RetryConfig{
            MaxAttempts: 4,
            Backoff:     relay.ExponentialBackoff(100*time.Millisecond, 2.0),
        }),
        // Observe the injected header in action
        relay.OnRequest(func(ctx context.Context, req *relay.RequestInfo) {
            key := req.Header.Get("X-Idempotency-Key")
            attempt := relay.AttemptFromContext(ctx)
            fmt.Printf("  attempt %d, idempotency key: %s\n", attempt, key)
        }),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Subscription creation - PUT is idempotent, safe to retry with same key
    fmt.Println("creating subscription:")
    resp, err := client.Put(context.Background(), "/subscriptions/sub-789/activate", map[string]interface{}{
        "plan": "pro_monthly",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("activation status:", resp.StatusCode)

    // Second subscription - NEW key is generated, independent of the first
    fmt.Println("\ncreating second subscription:")
    resp2, err := client.Put(context.Background(), "/subscriptions/sub-790/activate", map[string]interface{}{
        "plan": "starter_monthly",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer resp2.Body.Close()
    fmt.Println("activation status:", resp2.StatusCode)
}
```

---

## Which HTTP Methods Are Considered Safe

In the context of `WithAutoIdempotencyOnSafeRetries`, "safe" refers to HTTP methods where retrying the same request with the same key produces a predictable, non-duplicating result on a correctly implemented server.

| Method | Safe for retry | Idempotency key injected | Rationale |
|---|---|---|---|
| GET | Yes (naturally) | No - not needed | Reads have no side effects |
| HEAD | Yes (naturally) | No - not needed | Reads have no side effects |
| OPTIONS | Yes (naturally) | No - not needed | Metadata, no side effects |
| PUT | Yes | Yes | Full resource replacement; same payload, same key = same result |
| DELETE | Yes | Yes | Deleting the same resource twice is idempotent at the resource level |
| POST | No | No - use manual keys | Creates new resources; retry may duplicate |
| PATCH | No | No - use manual keys | Partial updates may not be idempotent |

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://catalog.internal"),
        relay.WithAutoIdempotencyOnSafeRetries(),
        relay.WithRetry(relay.RetryConfig{
            MaxAttempts: 3,
            Backoff:     relay.LinearBackoff(200 * time.Millisecond),
        }),
    )
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()

    // GET - no idempotency key needed, naturally safe
    resp, err := client.Get(ctx, "/products/prod-42", nil)
    if err != nil {
        log.Fatal(err)
    }
    resp.Body.Close()
    fmt.Println("GET ok:", resp.StatusCode)

    // PUT - idempotency key auto-injected
    resp, err = client.Put(ctx, "/products/prod-42", map[string]interface{}{
        "name":  "Widget Pro",
        "price": 29.99,
    })
    if err != nil {
        log.Fatal(err)
    }
    resp.Body.Close()
    fmt.Println("PUT ok:", resp.StatusCode)

    // DELETE - idempotency key auto-injected
    resp, err = client.Delete(ctx, "/products/prod-42", nil)
    if err != nil {
        log.Fatal(err)
    }
    resp.Body.Close()
    fmt.Println("DELETE ok:", resp.StatusCode)
}
```

---

## Custom Idempotency Key Generator Function

By default, `relay` generates UUID v4 keys using a cryptographically secure random source. You can replace the generator with your own function - for example, to include a request-specific deterministic component for traceability.

```go
package main

import (
    "context"
    "crypto/rand"
    "encoding/hex"
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

// deterministicKeyGenerator creates a key that incorporates the current
// millisecond timestamp and a random component, making it sortable and unique.
func deterministicKeyGenerator() (string, error) {
    randomBytes := make([]byte, 12)
    if _, err := rand.Read(randomBytes); err != nil {
        return "", fmt.Errorf("generate random bytes: %w", err)
    }
    timestamp := time.Now().UnixMilli()
    return fmt.Sprintf("%013x-%s", timestamp, hex.EncodeToString(randomBytes)), nil
}

func main() {
    client, err := relay.New(
        relay.WithBaseURL("https://ledger.internal"),
        relay.WithAutoIdempotencyOnSafeRetries(),
        relay.WithIdempotencyKeyGenerator(deterministicKeyGenerator),
        relay.WithRetry(relay.RetryConfig{
            MaxAttempts: 3,
            Backoff:     relay.ExponentialBackoff(100*time.Millisecond, 2.0),
        }),
        relay.OnRequest(func(ctx context.Context, req *relay.RequestInfo) {
            fmt.Printf("key: %s\n", req.Header.Get("X-Idempotency-Key"))
        }),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Put(context.Background(), "/ledger/entries/ent-001", map[string]interface{}{
        "amount":      500_00,
        "currency":    "USD",
        "description": "Q4 adjustment",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()
    fmt.Println("ledger entry status:", resp.StatusCode)
}
```

> **tip**
> Custom key generators are useful in scenarios where you need idempotency keys to be traceable in logs. A key that embeds a timestamp or trace ID makes it easier to correlate a specific HTTP call with the idempotency record on the server side.

---

## Why Idempotency Matters for Retries

Without idempotency keys, retrying failed requests is dangerous for any operation with side effects. Consider this sequence of events with a payment API:

```
T=0ms   Client sends PUT /payments/pay-001 {amount: 100}
T=50ms  Server processes payment, charges card, stores result
T=51ms  TCP connection drops (network blip)
T=52ms  Client never receives response, gets io.EOF or context deadline exceeded
T=52ms  Client retries (attempt 2)
T=102ms Server receives second PUT /payments/pay-001 {amount: 100}
        Without idempotency: server charges card AGAIN
        With idempotency: server returns cached response from T=50ms, no duplicate charge
```

With `WithAutoIdempotencyOnSafeRetries`:

```
T=0ms   relay generates key: "7f6bcd4a-..." and sends PUT with X-Idempotency-Key: 7f6bcd4a-...
T=50ms  Server processes, stores response keyed to "7f6bcd4a-..."
T=51ms  Connection drops
T=52ms  relay retries with the SAME key: X-Idempotency-Key: 7f6bcd4a-...
T=102ms Server recognizes key, returns cached 200 response
        Card is charged exactly once
```

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/jhonsferg/relay"
)

func main() {
    // With idempotency keys, retrying PUT is safe even for payment operations
    client, err := relay.New(
        relay.WithBaseURL("https://payments.internal"),
        relay.WithAutoIdempotencyOnSafeRetries(),
        relay.WithRetry(relay.RetryConfig{
            MaxAttempts: 5,
            Backoff:     relay.ExponentialBackoff(500*time.Millisecond, 2.0),
            // Retry on connection errors and 5xx responses
            RetryOn: relay.DefaultRetryPolicy,
        }),
        relay.WithTimeout(30 * time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Put(context.Background(), "/payments/pay-001/capture", map[string]interface{}{
        "amount":   10000,
        "currency": "USD",
    })
    if err != nil {
        log.Fatal("payment capture failed after all retries:", err)
    }
    defer resp.Body.Close()
    fmt.Println("payment captured, status:", resp.StatusCode)
}
```

---

## Idempotency Key Format (UUID v4)

By default, `relay` generates RFC 4122 version 4 (random) UUIDs as idempotency keys. UUID v4 provides 122 bits of randomness, making collisions astronomically unlikely (probability of collision with 1 trillion UUIDs is approximately 10^-18).

The format is:
```
xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
```
where `x` is a random hexadecimal digit and `y` is one of `8`, `9`, `a`, or `b`.

Example keys:
```
550e8400-e29b-41d4-a716-446655440000
f47ac10b-58cc-4372-a567-0e02b2c3d479
6ba7b810-9dad-11d1-80b4-00c04fd430c8
```

UUID v4 keys are accepted by Stripe, PayPal, Adyen, Braintree, GitHub, and virtually every API that supports idempotency.

```go
package main

import (
    "fmt"

    "github.com/jhonsferg/relay/internal/idempotency"
)

func main() {
    // Demonstrate the default key format
    for i := 0; i < 5; i++ {
        key, err := idempotency.GenerateKey()
        if err != nil {
            panic(err)
        }
        fmt.Println(key)
    }
    // Output (random each run):
    // 550e8400-e29b-41d4-a716-446655440000
    // f47ac10b-58cc-4372-a567-0e02b2c3d479
    // ...
}
```

---

## Server-Side Idempotency Requirements

Automatic idempotency keys are only useful if the server implements the server-side of the contract. For `relay`'s idempotency feature to provide safety guarantees, the server must:

1. **Store the response**: After processing the first request, the server stores the response (status code, headers, body) keyed by the idempotency key.
2. **Return cached response on duplicate**: If a request arrives with the same idempotency key and the same endpoint, the server returns the stored response without re-executing the operation.
3. **Respect the key scope**: The key must be scoped to the same endpoint and (usually) the same authenticated user. A key used for `PUT /orders/ord-1` should not match a request to `PUT /invoices/inv-1`.
4. **Enforce a TTL**: Idempotency records should expire (typically 24h to 7 days) to prevent unbounded storage growth.

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "time"

    "github.com/jhonsferg/relay"
)

// Example of a server handler that implements idempotency (illustrative).
// This shows what the server-side contract looks like.
func exampleServerHandler(store map[string][]byte) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        key := r.Header.Get("X-Idempotency-Key")
        if key != "" {
            if cached, ok := store[key]; ok {
                // Return cached response - operation already completed
                w.Header().Set("X-Idempotency-Replayed", "true")
                w.WriteHeader(http.StatusOK)
                w.Write(cached)
                return
            }
        }

        // Process the request
        result := map[string]interface{}{
            "id":         "ord-" + time.Now().Format("20060102150405"),
            "status":     "confirmed",
            "created_at": time.Now().Format(time.RFC3339),
        }
        responseBody, _ := json.Marshal(result)

        // Store the response for future duplicate requests
        if key != "" {
            store[key] = responseBody
        }

        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusOK)
        w.Write(responseBody)
    }
}

func main() {
    // Client side: use relay with auto idempotency
    client, err := relay.New(
        relay.WithBaseURL("https://orders.internal"),
        relay.WithAutoIdempotencyOnSafeRetries(),
        relay.WithRetry(relay.RetryConfig{
            MaxAttempts: 3,
            Backoff:     relay.ExponentialBackoff(200*time.Millisecond, 2.0),
        }),
    )
    if err != nil {
        log.Fatal(err)
    }

    resp, err := client.Put(context.Background(), "/orders/ord-123/confirm", map[string]interface{}{
        "payment_method_id": "pm_abc123",
    })
    if err != nil {
        log.Fatal(err)
    }
    defer resp.Body.Close()

    // Check if this was a replayed response
    if resp.Header.Get("X-Idempotency-Replayed") == "true" {
        fmt.Println("server returned cached (replayed) response - duplicate detected and handled")
    } else {
        fmt.Println("server processed request fresh, status:", resp.StatusCode)
    }
}
```

> **warning**
> `relay` cannot guarantee idempotency on its own - it can only inject the key. If the server does not implement the server-side idempotency contract, injecting `X-Idempotency-Key` has no safety effect. Always verify that your server-side implementation stores and returns cached responses correctly before relying on retry safety.

---

## Summary

| Feature | API |
|---|---|
| Enable auto idempotency keys | `WithAutoIdempotencyOnSafeRetries()` |
| Header injected | `X-Idempotency-Key: <uuid-v4>` |
| Safe methods (key injected) | PUT, DELETE |
| Unsafe methods (no key) | POST, PATCH |
| Naturally safe (no key needed) | GET, HEAD, OPTIONS |
| Custom key generator | `WithIdempotencyKeyGenerator(fn)` |
| Default key format | UUID v4 (RFC 4122) |
| Server requirement | Must cache and replay on duplicate key |
