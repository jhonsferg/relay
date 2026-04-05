# Request Deduplication

Request deduplication prevents duplicate network calls when multiple goroutines concurrently request the same resource. Using Go's `singleflight` package, relay ensures only one real HTTP call is made - all concurrent callers receive the same response.

## When to Use

- High-traffic endpoints where cache stampedes are common
- Parallel service calls that may duplicate reads
- Any GET/HEAD endpoint accessed by many goroutines simultaneously

## Enabling Deduplication

Deduplication is **opt-in** and only applies to safe HTTP methods (GET and HEAD).

```go
client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relay.WithConfig(relay.Config{
        Deduplication: relay.DeduplicationConfig{
            Enabled: true,
        },
    }),
)
```

Or via the config struct directly:

```go
client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
)
// Enable per-request:
resp, err := client.Execute(
    client.Get("/users/42").WithDeduplication(),
)
```

## How It Works

When deduplication is enabled and multiple goroutines call the same URL simultaneously:

1. The first goroutine executes the real HTTP request
2. All subsequent goroutines wait for the first to complete
3. All goroutines receive a copy of the same response body
4. Only **one** network call is made

The deduplication key is `method + URL` (including query parameters).

```
Goroutine A ─┐
Goroutine B ─┼─► [singleflight] ─► 1 HTTP request ─► response copy to A, B, C
Goroutine C ─┘
```

## Behavior Details

| Aspect | Behavior |
|--------|----------|
| Methods | GET and HEAD only |
| Key | `method + full URL` |
| Context cancellation | One caller cancelling does NOT cancel others |
| Response body | Each caller gets an independent copy |
| Errors | Shared - if the request fails, all callers get the same error |
| POST/PUT/DELETE | Always executed independently (never deduplicated) |

## Example: Cache Stampede Prevention

```go
client := relay.New(
    relay.WithBaseURL("https://api.example.com"),
    relay.WithConfig(relay.Config{
        Deduplication: relay.DeduplicationConfig{Enabled: true},
    }),
)

var wg sync.WaitGroup
for i := 0; i < 100; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        resp, err := client.Execute(client.Get("/config"))
        // All 100 goroutines get the response, but only 1 HTTP call is made.
        _ = resp
        _ = err
    }()
}
wg.Wait()
```

## Per-Request Control

```go
// Enable on a specific request:
resp, err := client.Execute(
    client.Get("/shared-resource").WithDeduplication(true),
)

// Disable on a specific request (overrides client config):
resp, err := client.Execute(
    client.Get("/unique-resource").WithDeduplication(false),
)
```

## Considerations

- Not suitable for endpoints that must always reflect the latest data
- The shared response is a snapshot - subsequent updates are not reflected
- Combine with caching extensions for longer-lived deduplication
