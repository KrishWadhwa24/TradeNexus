package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// keyCachePopulating is a flag that is true while the cacher is running.
	keyCachePopulating = "cache:populating"
	// keyCacheInstrumentCandles is the key for a single instrument's candles.
	keyCacheInstrumentCandles = "cache:instrument:%d:candles"
)

// Redis wraps a go-redis client.
type Redis struct {
	Client *redis.Client
}

// NewRedis connects to Redis and verifies it with a ping.
func NewRedis(ctx context.Context, addr, password string, db int) (*Redis, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return &Redis{Client: client}, nil
}

// Ping checks connectivity — used by the readiness endpoint.
func (r *Redis) Ping(ctx context.Context) error {
	return r.Client.Ping(ctx).Err()
}

// Close closes the client.
func (r *Redis) Close() error {
	if r.Client != nil {
		return r.Client.Close()
	}
	return nil
}

// SetCachedCandles stores the serialized candle data for an instrument.
func (r *Redis) SetCachedCandles(ctx context.Context, instrumentID int64, data []byte, ttl time.Duration) error {
	key := fmt.Sprintf(keyCacheInstrumentCandles, instrumentID)
	return r.Client.Set(ctx, key, data, ttl).Err()
}

// GetCachedCandles retrieves the serialized candle data for an instrument.
// It returns redis.Nil if the key does not exist.
func (r *Redis) GetCachedCandles(ctx context.Context, instrumentID int64) ([]byte, error) {
	key := fmt.Sprintf(keyCacheInstrumentCandles, instrumentID)
	return r.Client.Get(ctx, key).Bytes()
}

// SetCachePopulating sets or clears the cache populating flag.
func (r *Redis) SetCachePopulating(ctx context.Context, status bool, ttl time.Duration) error {
	if !status {
		return r.Client.Del(ctx, keyCachePopulating).Err()
	}
	return r.Client.Set(ctx, keyCachePopulating, "true", ttl).Err()
}

// IsCachePopulating checks if the cache populating flag is set.
func (r *Redis) IsCachePopulating(ctx context.Context) (bool, error) {
	val, err := r.Client.Get(ctx, keyCachePopulating).Result()
	if err == redis.Nil {
		return false, nil // Not found means not populating.
	}
	if err != nil {
		return false, err
	}
	return val == "true", nil
}

// Get retrieves a value from Redis. It returns redis.Nil if the key does not exist.
func (r *Redis) Get(ctx context.Context, key string) ([]byte, error) {
	return r.Client.Get(ctx, key).Bytes()
}

// Set stores a value in Redis. The value is marshaled to JSON.
func (r *Redis) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	p, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	return r.Client.Set(ctx, key, p, ttl).Err()
}

// SetBytes stores raw bytes in Redis.
func (r *Redis) SetBytes(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return r.Client.Set(ctx, key, value, ttl).Err()
}
