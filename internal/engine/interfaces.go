package engine

import (
	"context"
	"time"

	"tradenexus/internal/market"
	"tradenexus/internal/signals"
)

// Candler defines the interface for candle data operations.
type Candler interface {
	GetDaily(ctx context.Context, instrumentID int64) ([]market.Candle, error)
	GetAggregates(ctx context.Context, instrumentID int64, tf string) ([]market.AggCandle, error)
	RebuildAggregates(ctx context.Context, instrumentID int64) (int, int, error)
	DailyDateSet(ctx context.Context, instrumentID int64) (map[string]bool, time.Time, time.Time, bool, error)
	UpsertDaily(ctx context.Context, instrumentID int64, candles []market.Candle) (int, error)
	ListInstrumentIDsWithData(ctx context.Context) ([]int64, error)
}

// Signaler defines the interface for signal data operations.
type Signaler interface {
	Upsert(ctx context.Context, s signals.Signal) (bool, int64, error)
	DeleteOlderThan(ctx context.Context, retention time.Duration) (int64, error)
}

// Redis defines the interface for Redis operations.
type Redis interface {
	IsCachePopulating(ctx context.Context) (bool, error)
	GetCachedCandles(ctx context.Context, instrumentID int64) ([]byte, error)
	SetCachedCandles(ctx context.Context, instrumentID int64, data []byte, ttl time.Duration) error
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	SetBytes(ctx context.Context, key string, value []byte, ttl time.Duration) error
}
