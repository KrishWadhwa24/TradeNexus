// Package ratelimit implements a distributed token-bucket limiter backed by
// Redis. It exists primarily to keep us under Angel One SmartAPI's request
// limits (the historical endpoint is the tightest bucket).
//
// Why Redis and not a local limiter: the bucket is shared. Even though we run
// locally today, the sync engine, reconciliation, and any worker all pull from
// ONE Angel session, so they must share ONE rate budget. A Redis bucket makes
// that correct regardless of how many goroutines/processes call Angel.
//
// The refill + take is done in a single atomic Lua script so concurrent callers
// can never over-spend the budget.
package ratelimit

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
)

// tokenBucketLua atomically refills the bucket based on elapsed time and, if
// enough tokens are available, takes `requested` of them.
//
// KEYS[1] = bucket key
// ARGV[1] = rate       (tokens per second, float)
// ARGV[2] = capacity   (max tokens / burst, float)
// ARGV[3] = now_ms     (current time, ms)
// ARGV[4] = requested  (tokens to take, float)
//
// returns { allowed (1|0), retry_after_ms }
const tokenBucketLua = `
local key       = KEYS[1]
local rate      = tonumber(ARGV[1])
local capacity  = tonumber(ARGV[2])
local now       = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

local data   = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts     = tonumber(data[2])
if tokens == nil then tokens = capacity end
if ts == nil then ts = now end

local elapsed = math.max(0, now - ts) / 1000.0
tokens = math.min(capacity, tokens + elapsed * rate)

local allowed = 0
local retry_ms = 0
if tokens >= requested then
    tokens = tokens - requested
    allowed = 1
else
    local deficit = requested - tokens
    retry_ms = math.ceil((deficit / rate) * 1000.0)
end

redis.call('HSET', key, 'tokens', tokens, 'ts', now)
-- Expire idle buckets once they'd be full again (+1s slack).
local ttl = math.ceil(capacity / rate) + 1
redis.call('PEXPIRE', key, ttl * 1000)

return { allowed, retry_ms }
`

// Limiter is a single named token bucket.
type Limiter struct {
	rdb      *redis.Client
	script   *redis.Script
	key      string
	rate     float64 // tokens per second
	capacity float64 // burst
}

// New builds a limiter. rate = sustained requests/sec, burst = max short spike.
func New(rdb *redis.Client, key string, rate float64, burst int) *Limiter {
	if rate <= 0 {
		rate = 1
	}
	if burst <= 0 {
		burst = 1
	}
	return &Limiter{
		rdb:      rdb,
		script:   redis.NewScript(tokenBucketLua),
		key:      "ratelimit:" + key,
		rate:     rate,
		capacity: float64(burst),
	}
}

// Allow tries to take n tokens without blocking. It returns whether the take
// succeeded and, if not, how long to wait before the tokens would be available.
func (l *Limiter) Allow(ctx context.Context, n int) (ok bool, retryAfter time.Duration, err error) {
	nowMs := time.Now().UnixMilli()
	res, err := l.script.Run(ctx, l.rdb, []string{l.key},
		l.rate, l.capacity, nowMs, n).Result()
	if err != nil {
		return false, 0, fmt.Errorf("ratelimit eval: %w", err)
	}
	vals, ok2 := res.([]interface{})
	if !ok2 || len(vals) != 2 {
		return false, 0, fmt.Errorf("ratelimit: unexpected script result %v", res)
	}
	allowed, _ := vals[0].(int64)
	retryMs, _ := vals[1].(int64)
	return allowed == 1, time.Duration(retryMs) * time.Millisecond, nil
}

// Wait blocks until n tokens can be taken or the context is cancelled.
// This is what the Angel fetcher uses so bursts queue instead of failing.
func (l *Limiter) Wait(ctx context.Context, n int) error {
	for {
		ok, retry, err := l.Allow(ctx, n)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if retry <= 0 {
			retry = time.Duration(math.Ceil(1000.0/l.rate)) * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retry):
		}
	}
}
