// Package intraday caches today's (still-forming) daily candle for each tracked
// stock in Redis while the market is open, fetched from the Angel historical
// API. Manual scans then combine this live candle with the confirmed daily
// history in Postgres — no per-scan Angel hammering, no rate-limit storms.
//
// Refresh strategy: keys are OVERWRITTEN in place (SET with TTL), never deleted
// first. So a scan reading mid-refresh always sees the previous or the new
// value for each stock — never an empty window. A Redis lock (SET NX) prevents
// two refreshes running at once.
package intraday

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"tradenexus/internal/angel"
	"tradenexus/internal/calendar"
	"tradenexus/internal/candles"
	"tradenexus/internal/instruments"
	"tradenexus/internal/market"
)

const (
	keyPrefix = "intraday:candle:"
	lockKey   = "intraday:lock"
	builtKey  = "intraday:built_at"
)

// Cache manages today's forming candle per instrument in Redis.
type Cache struct {
	rdb     *redis.Client
	angel   *angel.Client
	inst    *instruments.Repo
	candles *candles.Repo
	cal     *calendar.Service
	ttl     time.Duration
	log     zerolog.Logger
	mu      sync.Mutex // serializes local refreshes
}

// New builds the cache. ttl should exceed the refresh interval so keys survive
// between refreshes.
func New(rdb *redis.Client, ang *angel.Client, inst *instruments.Repo, c *candles.Repo,
	cal *calendar.Service, ttl time.Duration, log zerolog.Logger) *Cache {
	if ttl <= 0 {
		ttl = 25 * time.Minute
	}
	return &Cache{rdb: rdb, angel: ang, inst: inst, candles: c, cal: cal, ttl: ttl, log: log}
}

func key(id int64) string { return fmt.Sprintf("%s%d", keyPrefix, id) }

// MarketOpen reports whether NSE cash is currently open.
func (c *Cache) MarketOpen(now time.Time) bool { return c.cal.Cal().IsMarketOpen(now) }

// Get returns today's cached forming candle for an instrument, if present.
func (c *Cache) Get(ctx context.Context, instrumentID int64) (market.Candle, bool, error) {
	b, err := c.rdb.Get(ctx, key(instrumentID)).Bytes()
	if err == redis.Nil {
		return market.Candle{}, false, nil
	}
	if err != nil {
		return market.Candle{}, false, err
	}
	var cd market.Candle
	if err := json.Unmarshal(b, &cd); err != nil {
		return market.Candle{}, false, err
	}
	return cd, true, nil
}

// fetchToday pulls today's (forming) daily candle for an instrument from Angel.
func (c *Cache) fetchToday(ctx context.Context, inst instruments.Instrument) (market.Candle, bool, error) {
	now := time.Now().In(market.IST)
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, market.IST)
	cs, err := c.angel.GetDailyCandles(ctx, inst.Exchange, inst.SymbolToken, from, now)
	if err != nil {
		return market.Candle{}, false, err
	}
	if len(cs) == 0 {
		return market.Candle{}, false, nil
	}
	return cs[len(cs)-1], true, nil // last = today's forming candle
}

// Refresh fetches today's candle for every tracked instrument and overwrites
// its cache key. Guarded by a Redis lock so concurrent refreshes are skipped.
func (c *Cache) Refresh(ctx context.Context) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ok, err := c.rdb.SetNX(ctx, lockKey, "1", 3*time.Minute).Result()
	if err != nil {
		return 0, err
	}
	if !ok {
		c.log.Debug().Msg("intraday: refresh already in progress; skipping")
		return 0, nil
	}
	defer c.rdb.Del(context.Background(), lockKey)

	ids, err := c.candles.ListInstrumentIDsWithData(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		inst, err := c.inst.GetByID(ctx, id)
		if err != nil {
			continue
		}
		cd, found, err := c.fetchToday(ctx, inst)
		if err != nil || !found {
			continue // leave the previous cached value in place
		}
		b, _ := json.Marshal(cd)
		if err := c.rdb.Set(ctx, key(id), b, c.ttl).Err(); err != nil {
			continue
		}
		n++
	}
	c.rdb.Set(ctx, builtKey, time.Now().In(market.IST).Format(time.RFC3339), c.ttl)
	c.log.Info().Int("cached", n).Int("tracked", len(ids)).Msg("intraday: cache refreshed")
	return n, nil
}

// EnsureBuilt refreshes the cache only if it's currently empty — used when the
// first manual scan of a session runs while the market is open.
func (c *Cache) EnsureBuilt(ctx context.Context) error {
	exists, err := c.rdb.Exists(ctx, builtKey).Result()
	if err != nil {
		return err
	}
	if exists == 1 {
		return nil
	}
	_, err = c.Refresh(ctx)
	return err
}
