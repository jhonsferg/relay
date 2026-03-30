// Package distributed provides a Redis-backed sliding-window rate limiter for
// relay HTTP clients. Unlike the built-in token-bucket rate limiter (which is
// per-process), this limiter is shared across multiple instances of your
// service using a common Redis cluster.
//
// # Algorithm
//
// The sliding window is implemented using a Redis sorted set:
//  1. Remove entries older than the window (ZREMRANGEBYSCORE).
//  2. Count remaining entries (ZCARD).
//  3. If count < limit: add the current request (ZADD) and set the key TTL.
//  4. If count ≥ limit: reject the request.
//
// Steps 1–4 are executed atomically in a Lua script to avoid race conditions.
//
// # Usage
//
//	import (
//	    "github.com/redis/go-redis/v9"
//	    "github.com/jhonsferg/relay"
//	    relaydist "github.com/jhonsferg/relay/ext/ratelimit/distributed"
//	)
//
//	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
//
//	// Allow at most 100 requests per second per client IP.
//	limiter := relaydist.New(rdb, "myapp:rl:global", 100, time.Second)
//
//	client := relay.New(
//	    relay.WithBaseURL("https://api.example.com"),
//	    relaydist.WithRateLimit(limiter),
//	)
//
// # Key naming
//
// key is used as-is as the Redis key. Use distinct keys for different rate
// limit buckets (per-user, per-IP, per-endpoint):
//
//	limiter := relaydist.New(rdb, "rl:user:"+userID, 10, time.Minute)
//
// # Error behavior
//
// When the rate limit is exceeded, [relay.Client.Execute] returns
// [ErrRateLimited]. When Redis is unavailable the limiter fails open (allows
// the request) to avoid cascading failures.
package distributed

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	redisclient "github.com/redis/go-redis/v9"

	"github.com/jhonsferg/relay"
)

// ErrRateLimited is returned by [relay.Client.Execute] when the distributed
// rate limit is exceeded.
var ErrRateLimited = errors.New("distributed rate limit exceeded")

// slidingWindowScript is the Lua script that atomically checks and increments
// the sliding-window counter in Redis.
//
// KEYS[1] = sorted-set key
// ARGV[1] = limit (int)
// ARGV[2] = window size in milliseconds (int)
// ARGV[3] = current timestamp in milliseconds (int)
// ARGV[4] = unique ID for this request (string)
//
// Returns: 1 if allowed, 0 if rate-limited.
var slidingWindowScript = redisclient.NewScript(`
local key    = KEYS[1]
local limit  = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local now    = tonumber(ARGV[3])
local id     = ARGV[4]

-- Remove entries outside the sliding window.
redis.call('ZREMRANGEBYSCORE', key, '-inf', now - window)

-- Count remaining entries in the window.
local count = redis.call('ZCARD', key)

if count >= limit then
    return 0
end

-- Record this request and reset the TTL.
redis.call('ZADD', key, now, id)
redis.call('PEXPIRE', key, window)

return 1
`)

// RateLimiter is a Redis-backed sliding-window rate limiter.
// Construct via [New]; all methods are safe for concurrent use.
type RateLimiter struct {
	rdb    redisclient.Cmdable
	key    string
	limit  int
	window time.Duration
}

// New creates a [RateLimiter] that allows at most limit requests per window
// across all processes sharing the same Redis key.
//
//   - rdb: any redis.Cmdable (*redis.Client, *redis.ClusterClient, …).
//   - key: Redis key for the sorted set (unique per rate-limit bucket).
//   - limit: maximum number of requests allowed within window.
//   - window: duration of the sliding window (e.g. time.Second, time.Minute).
func New(rdb redisclient.Cmdable, key string, limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{rdb: rdb, key: key, limit: limit, window: window}
}

// Allow checks whether the next request is within the rate limit. It returns
// nil if the request is allowed and [ErrRateLimited] if the window is full.
// On a Redis error it fails open (returns nil) to avoid blocking all traffic
// when the rate-limit store is unavailable.
func (r *RateLimiter) Allow(ctx context.Context) error {
	now := time.Now().UnixMilli()
	windowMS := r.window.Milliseconds()
	id := randomID()

	result, err := slidingWindowScript.Run(ctx, r.rdb,
		[]string{r.key},
		r.limit, windowMS, now, id,
	).Int()
	if err != nil {
		// Fail open on Redis errors (connection refused, timeout, etc.).
		return nil
	}
	if result == 0 {
		return ErrRateLimited
	}
	return nil
}

// WithRateLimit returns a [relay.Option] that enforces the distributed rate
// limit on every request via an [OnBeforeRequest] hook. The hook checks [Allow]
// and returns [ErrRateLimited] if the budget is exhausted, which causes
// [relay.Client.Execute] to return that error without making a network call.
func WithRateLimit(limiter *RateLimiter) relay.Option {
	return relay.WithOnBeforeRequest(func(ctx context.Context, req *relay.Request) error {
		if err := limiter.Allow(ctx); err != nil {
			return fmt.Errorf("%w", err)
		}
		return nil
	})
}

// randomID returns a 16-hex-character random string used to uniquely identify
// each request in the sorted set, preventing duplicate key collisions.
func randomID() string {
	b := make([]byte, 8)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}
