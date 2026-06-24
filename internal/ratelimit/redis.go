package ratelimit

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// luaScript implements fixed-window rate limiting atomically.
//
// KEYS[1]: full rate-limit key (base key + ":" + windowStart Unix nanoseconds)
// ARGV[1]: expiry Unix-millisecond timestamp (windowStart + window), for key cleanup
// ARGV[2]: per-window request limit
//
// Returns {1, 0} when allowed; {0, PTTL} when over limit (PTTL is milliseconds
// until the current window expires and the counter resets).
const luaScript = `
local count = redis.call('INCR', KEYS[1])
if count == 1 then redis.call('PEXPIREAT', KEYS[1], ARGV[1]) end
if count > tonumber(ARGV[2]) then return {0, redis.call('PTTL', KEYS[1])} end
return {1, 0}
`

type redisLimiter struct {
	client redis.UniversalClient
	window time.Duration
	script *redis.Script
}

// NewRedisLimiter returns a Limiter backed by Redis using a fixed-window
// algorithm with atomic Lua script execution. client must be an open
// redis.UniversalClient (single-node, Sentinel, or Cluster). Lua scripts use
// a single key per call, which is safe for Redis Cluster slot assignment.
func NewRedisLimiter(client redis.UniversalClient, window time.Duration) Limiter {
	return &redisLimiter{
		client: client,
		window: window,
		script: redis.NewScript(luaScript),
	}
}

func (r *redisLimiter) Allow(ctx context.Context, key string, limit int) (bool, time.Duration, error) {
	now := time.Now()
	windowStart := now.Truncate(r.window)
	fullKey := key + ":" + strconv.FormatInt(windowStart.UnixNano(), 10)
	expireAtMs := windowStart.Add(r.window).UnixMilli()

	result, err := r.script.Run(ctx, r.client, []string{fullKey}, expireAtMs, limit).Int64Slice()
	if err != nil {
		return true, 0, err
	}
	if result[0] == 0 {
		ttl := time.Duration(result[1]) * time.Millisecond
		if ttl <= 0 {
			ttl = r.window
		}
		return false, ttl, nil
	}
	return true, 0, nil
}

func (r *redisLimiter) Close() error {
	return r.client.Close()
}
