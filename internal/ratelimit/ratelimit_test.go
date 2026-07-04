package ratelimit

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// testRedis connects to a local Redis, or skips the test if none is reachable.
// Bring one up with `docker compose up -d redis` before running.
func testRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	c := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available at %s: %v", addr, err)
	}
	return c
}

func TestAllow_BurstThenExhaust(t *testing.T) {
	rdb := testRedis(t)
	defer rdb.Close()
	ctx := context.Background()

	key := "test:burst:" + time.Now().Format("150405.000000")
	defer rdb.Del(ctx, "ratelimit:"+key)

	// rate 5/s, burst 3 → first 3 take immediately, 4th is denied.
	l := New(rdb, key, 5, 3)

	for i := 0; i < 3; i++ {
		ok, _, err := l.Allow(ctx, 1)
		if err != nil {
			t.Fatalf("allow %d: %v", i, err)
		}
		if !ok {
			t.Fatalf("expected token %d to be allowed within burst", i)
		}
	}

	ok, retry, err := l.Allow(ctx, 1)
	if err != nil {
		t.Fatalf("allow 4th: %v", err)
	}
	if ok {
		t.Fatal("expected 4th token to be denied (burst exhausted)")
	}
	if retry <= 0 || retry > time.Second {
		t.Fatalf("expected small positive retry, got %v", retry)
	}
}

func TestWait_Blocks(t *testing.T) {
	rdb := testRedis(t)
	defer rdb.Close()
	ctx := context.Background()

	key := "test:wait:" + time.Now().Format("150405.000000")
	defer rdb.Del(ctx, "ratelimit:"+key)

	// rate 10/s, burst 1 → after taking the single token, Wait must block ~100ms.
	l := New(rdb, key, 10, 1)

	if err := l.Wait(ctx, 1); err != nil {
		t.Fatalf("first wait: %v", err)
	}
	start := time.Now()
	if err := l.Wait(ctx, 1); err != nil {
		t.Fatalf("second wait: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("expected Wait to block for refill, only waited %v", elapsed)
	}
}
