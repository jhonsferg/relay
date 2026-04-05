# Redis Cache Extension

The Redis Cache extension adds RFC 7234-compliant HTTP caching to relay clients backed by Redis. Cacheable responses are stored in Redis with a configurable TTL, and subsequent identical requests are served from cache without hitting the origin server. This dramatically reduces latency and API quota usage for read-heavy workloads.

**Import path:** `github.com/jhonsferg/relay/ext/cache`

---

## Overview

HTTP caching is governed by RFC 7234. The extension implements the most important rules:

- Responses with `Cache-Control: no-store` or `Pragma: no-cache` are never cached.
- Responses with `Cache-Control: no-cache` are stored but must be revalidated before serving.
- Conditional requests use `ETag` / `If-None-Match` and `Last-Modified` / `If-Modified-Since`.
- The `Vary` header controls which request headers are included in the cache key.
- Only configurable HTTP status codes are considered cacheable (default: 200, 203, 204, 206, 300, 301, 404, 405, 410, 414, 501).

The extension works at the transport layer, sitting between relay and the network. Cache hits never reach the HTTP stack at all.

---

## Installation

```bash
go get github.com/jhonsferg/relay/ext/cache@latest
go get github.com/redis/go-redis/v9@latest
```

---

## Configuration

### `relaycache.Config`

```go
type Config struct {
    // TTL is the default time-to-live for cached responses.
    // This is used when the response does not include a Cache-Control max-age
    // or Expires header. Defaults to 5 minutes.
    TTL time.Duration

    // CacheableStatus is the list of HTTP status codes that are eligible
    // for caching. Responses with any other status are never stored.
    // Defaults to [200, 203, 204, 206, 300, 301, 404, 405, 410, 414, 501].
    CacheableStatus []int

    // VaryHeaders is a list of request headers (beyond what the server
    // declares in its Vary response header) to include in the cache key.
    // Use this to partition the cache by tenant ID, API version, or locale.
    VaryHeaders []string

    // KeyPrefix is an optional string prepended to every Redis key.
    // Use this to namespace keys when multiple relay clients share a Redis instance.
    // Defaults to "relay:cache:".
    KeyPrefix string

    // MaxBodySize is the maximum response body size in bytes to cache.
    // Responses larger than this limit are passed through without caching.
    // Defaults to 1 MiB (1048576 bytes).
    MaxBodySize int64

    // CompressionEnabled compresses cached response bodies with gzip before
    // writing to Redis. This reduces memory usage at the cost of CPU time.
    // Defaults to false.
    CompressionEnabled bool

    // StaleWhileRevalidate is the duration beyond max-age during which a
    // stale cached response may be served while a background revalidation runs.
    // Mirrors the Cache-Control stale-while-revalidate directive.
    StaleWhileRevalidate time.Duration
}
```

### `relaycache.WithRedisCache`

```go
relaycache.WithRedisCache(client redis.UniversalClient, config *relaycache.Config) relay.Option
```

Attaches the cache extension to a relay client. `redis.UniversalClient` accepts any go-redis client type: `*redis.Client`, `*redis.ClusterClient`, or `*redis.Ring`.

---

## Complete Example with go-redis v9

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    relay "github.com/jhonsferg/relay"
    relaycache "github.com/jhonsferg/relay/ext/cache"
    "github.com/redis/go-redis/v9"
)

type GitHubUser struct {
    Login     string `json:"login"`
    ID        int    `json:"id"`
    Name      string `json:"name"`
    Company   string `json:"company"`
    Blog      string `json:"blog"`
    Location  string `json:"location"`
    Email     string `json:"email"`
    Bio       string `json:"bio"`
    Followers int    `json:"followers"`
    Following int    `json:"following"`
}

func main() {
    // Connect to Redis.
    rdb := redis.NewClient(&redis.Options{
        Addr:         "localhost:6379",
        Password:     "",
        DB:           0,
        DialTimeout:  2 * time.Second,
        ReadTimeout:  2 * time.Second,
        WriteTimeout: 2 * time.Second,
        PoolSize:     10,
        MinIdleConns: 2,
    })

    // Verify Redis connectivity before creating the client.
    ctx := context.Background()
    if err := rdb.Ping(ctx).Err(); err != nil {
        log.Fatalf("redis ping: %v", err)
    }

    client, err := relay.NewClient(
        relay.WithBaseURL("https://api.github.com"),
        relay.WithHeader("Accept", "application/vnd.github+json"),
        relay.WithHeader("X-GitHub-Api-Version", "2022-11-28"),
        relay.WithTimeout(10*time.Second),
        relaycache.WithRedisCache(rdb, &relaycache.Config{
            // Cache responses for 10 minutes by default.
            TTL: 10 * time.Minute,

            // Only cache successful responses and permanent redirects.
            CacheableStatus: []int{200, 301, 404},

            // Include the Accept and Accept-Language headers in the cache key
            // so that different response formats are cached separately.
            VaryHeaders: []string{"Accept", "Accept-Language"},

            // Prefix all Redis keys with "ghclient:" for easy identification.
            KeyPrefix: "ghclient:",

            // Do not cache responses larger than 512 KiB.
            MaxBodySize: 512 * 1024,

            // Compress cached bodies to save Redis memory.
            CompressionEnabled: true,

            // Serve stale responses for up to 30 seconds while revalidating.
            StaleWhileRevalidate: 30 * time.Second,
        }),
    )
    if err != nil {
        log.Fatalf("create client: %v", err)
    }

    // First call - hits the network.
    start := time.Now()
    var user GitHubUser
    if err := client.Get(ctx, "/users/torvalds", &user); err != nil {
        log.Fatalf("get user: %v", err)
    }
    fmt.Printf("First call (%s): %s - %s\n", time.Since(start), user.Login, user.Name)

    // Second call - served from Redis cache, much faster.
    start = time.Now()
    var user2 GitHubUser
    if err := client.Get(ctx, "/users/torvalds", &user2); err != nil {
        log.Fatalf("get user (cached): %v", err)
    }
    fmt.Printf("Second call (%s): %s - %s\n", time.Since(start), user2.Login, user2.Name)
}
```

---

## Cache Key Generation

The cache key combines several components to ensure correct cache partitioning:

1. **KeyPrefix** - configured namespace string
2. **HTTP method** - only GET and HEAD requests are cached by default
3. **Request URL** - full URL including scheme, host, path, and query string
4. **Vary header values** - values of headers listed in both the response `Vary` header and `VaryHeaders` config field, hashed together

The key is constructed as:

```
{KeyPrefix}{method}:{SHA256(normalizedURL + variedHeaderValues)}
```

For example, a GET to `https://api.github.com/users/torvalds` with `Accept: application/vnd.github+json` produces:

```
ghclient:GET:a3f8c21e9b4d...
```

The SHA256 hash ensures keys remain within Redis's key size limits even for long URLs.

> **Note:** Query parameter order is normalized before hashing. `?b=2&a=1` and `?a=1&b=2` produce the same cache key, matching RFC 7234 intent.

---

## RFC 7234 Cache-Control Compliance

### Request Directives

The extension respects the following `Cache-Control` directives in incoming requests:

| Directive           | Effect                                                          |
|---------------------|-----------------------------------------------------------------|
| `no-store`          | Skip cache lookup and do not store the response                 |
| `no-cache`          | Skip cache lookup (forces revalidation) but store the response  |
| `max-age=0`         | Treat cached entries as stale, force revalidation               |
| `only-if-cached`    | Return 504 if no cached entry exists                            |

### Response Directives

The extension respects the following `Cache-Control` directives in responses from the origin:

| Directive              | Effect                                                              |
|------------------------|---------------------------------------------------------------------|
| `no-store`             | Never cache this response                                           |
| `no-cache`             | Cache but always revalidate before serving                          |
| `private`              | Do not cache (shared caches must not store private responses)       |
| `max-age=N`            | Cache for N seconds (overrides configured TTL)                      |
| `s-maxage=N`           | Cache for N seconds (shared cache override, takes priority)         |
| `must-revalidate`      | Do not serve stale content even if origin is unreachable            |
| `stale-while-revalidate=N` | Serve stale for N seconds while revalidating in background      |

---

## Conditional Requests and Revalidation

When a cached response includes an `ETag` or `Last-Modified` header, the extension automatically sends conditional requests to the origin:

```
GET /users/torvalds HTTP/1.1
If-None-Match: "abc123etag"
If-Modified-Since: Tue, 01 Jan 2025 00:00:00 GMT
```

If the origin returns `304 Not Modified`, the cached response is refreshed (its TTL is extended) and served without re-downloading the body. This minimizes bandwidth usage for APIs that include ETags.

```go
// Revalidation is automatic. No special configuration needed.
// The extension handles If-None-Match and 304 responses transparently.
var user GitHubUser
err := client.Get(ctx, "/users/torvalds", &user)
// - First call: 200, body stored in Redis with ETag
// - Second call (after TTL): 304, Redis entry refreshed, body served from cache
```

---

## Cache Invalidation

### TTL-Based Invalidation

Cached entries expire automatically after their TTL. The TTL is determined in order of priority:

1. `Cache-Control: s-maxage=N` in the response
2. `Cache-Control: max-age=N` in the response
3. `Expires` header in the response
4. `Config.TTL` (fallback default)

### Manual Invalidation

Invalidate specific cache entries by calling `relaycache.Invalidate`:

```go
// Invalidate a single URL.
if err := relaycache.Invalidate(ctx, client, "GET", "https://api.github.com/users/torvalds"); err != nil {
    log.Printf("invalidate: %v", err)
}
```

Invalidate all entries with a given key prefix:

```go
// Invalidate all entries for the github client.
if err := relaycache.InvalidatePrefix(ctx, client, "ghclient:"); err != nil {
    log.Printf("invalidate prefix: %v", err)
}
```

### Post-Mutation Invalidation

Combine with the `relay/ext/graphql` mutation observer to automatically invalidate related cache entries after a write:

```go
relayoauth.WithMutationObserver(func(req *http.Request) {
    // After any POST/PUT/DELETE to /users/*, invalidate user cache entries.
    if strings.HasPrefix(req.URL.Path, "/users/") {
        _ = relaycache.InvalidatePrefix(context.Background(), client, "ghclient:GET:users/")
    }
})
```

---

## Redis Cluster and Sentinel

The extension accepts any `redis.UniversalClient`, which includes cluster and Sentinel configurations:

```go
// Redis Cluster
rdb := redis.NewClusterClient(&redis.ClusterOptions{
    Addrs:        []string{"redis-1:7000", "redis-2:7001", "redis-3:7002"},
    DialTimeout:  2 * time.Second,
    ReadTimeout:  2 * time.Second,
    WriteTimeout: 2 * time.Second,
    PoolSize:     20,
})

// Redis Sentinel (high availability)
rdb := redis.NewFailoverClient(&redis.FailoverOptions{
    MasterName:    "mymaster",
    SentinelAddrs: []string{"sentinel-1:26379", "sentinel-2:26379", "sentinel-3:26379"},
    PoolSize:      10,
})

// Both types satisfy redis.UniversalClient and work with relaycache.
client, _ := relay.NewClient(
    relay.WithBaseURL("https://api.example.com"),
    relaycache.WithRedisCache(rdb, &relaycache.Config{
        TTL:       5 * time.Minute,
        KeyPrefix: "myapp:",
    }),
)
```

---

## Cache Metrics

The extension exposes cache metrics that integrate with the relay observability layer:

```go
import (
    relay "github.com/jhonsferg/relay"
    relaycache "github.com/jhonsferg/relay/ext/cache"
    "github.com/prometheus/client_golang/prometheus"
)

reg := prometheus.NewRegistry()

client, _ := relay.NewClient(
    relay.WithBaseURL("https://api.example.com"),
    relaycache.WithRedisCache(rdb, &relaycache.Config{
        TTL: 5 * time.Minute,
    }),
    // relay's observability integration picks up cache hit/miss counters
    // automatically from the extension when this option is present.
    relay.WithPrometheusRegistry(reg),
)
```

Exported metrics:
- `relay_cache_hits_total` - count of requests served from cache
- `relay_cache_misses_total` - count of requests that bypassed cache
- `relay_cache_stale_total` - count of stale responses served
- `relay_cache_revalidations_total` - count of conditional requests sent
- `relay_cache_errors_total` - count of Redis errors (cache is bypassed on error)

---

## Bypassing the Cache for Specific Requests

Force a cache bypass for individual requests without reconfiguring the client:

```go
import "net/http"

ctx := context.Background()
req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/users/torvalds", nil)
// Standard HTTP Cache-Control request directive.
req.Header.Set("Cache-Control", "no-cache")

var user GitHubUser
if err := client.Do(req, &user); err != nil {
    log.Fatal(err)
}
// Response is fetched fresh from origin and updates the cache entry.
```

---

## Error Handling

Redis errors do not fail requests. If Redis is unavailable, the extension logs a warning and forwards the request to the origin transparently. This means your application continues to work during Redis outages at the cost of increased origin load.

```go
relaycache.WithRedisCache(rdb, &relaycache.Config{
    TTL: 5 * time.Minute,
    // OnError is called whenever a Redis operation fails.
    // Use this to alert, record metrics, or implement fallback logic.
    OnError: func(op string, err error) {
        log.Printf("cache %s error: %v", op, err)
        // alerting.Increment("relay.cache.redis_error")
    },
})
```

> **Warning:** If `Config.OnError` is nil and Redis is unavailable, errors are silently swallowed. Set an `OnError` handler in production to detect Redis connectivity issues early.

---

## See Also

- [OAuth2 Extension](oauth.md) - caching token responses
- [GraphQL Extension](graphql.md) - caching GraphQL query results
- relay core documentation - request deduplication (singleflight) that complements caching
