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
	"strings"
	"sync"
	"sync/atomic"
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

	// reconciling is a reference count, not a boolean: it's incremented for
	// the duration of every reconciliation call (bulk ReconcileAll, a single
	// admin Reconcile, or RefetchDate) and decremented when each finishes.
	// Refresh waits for it to reach zero before populating Redis, so
	// reconciliation and intraday population never compete for the same
	// Angel session/rate budget at once — reconciliation always goes first.
	// A refcount (rather than a bool) means overlapping/nested reconcile
	// calls don't clear the gate out from under each other.
	reconciling atomic.Int32
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

// BeginReconcile marks one reconciliation call as in progress (see the
// reconciling field doc). Call before starting it, with a matching
// EndReconcile via defer; any Refresh call (manual scan or the scheduled
// ticker) will block until every in-flight reconcile call has ended.
func (c *Cache) BeginReconcile() { c.reconciling.Add(1) }

// EndReconcile matches a BeginReconcile call.
func (c *Cache) EndReconcile() { c.reconciling.Add(-1) }

// waitForReconcile blocks while any reconciliation call is in progress.
func (c *Cache) waitForReconcile(ctx context.Context) error {
	for c.reconciling.Load() > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil
}

// isRateLimitErr reports whether an error from Angel's historical endpoint
// looks like throttling (HTTP 429/403 or app-level AB1004), based on the
// message text produced by angel.GetDailyCandles.
func isRateLimitErr(err error) bool {
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "rate limit") ||
		strings.Contains(m, "access rate") ||
		strings.Contains(m, "ab1004") ||
		strings.Contains(m, "too many")
}

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

// refreshWorkers bounds how many instruments are fetched from Angel at once.
// Each worker still waits on the same shared rate limiter (burst=1), so this
// overlaps request latency across stocks rather than raising the true
// dispatch rate to Angel.
const refreshWorkers = 5

// Refresh fetches today's candle for every tracked instrument and overwrites
// its cache key. Guarded by a Redis lock so concurrent refreshes are skipped.
// Waits out any in-progress reconciliation first (see BeginReconcile).
func (c *Cache) Refresh(ctx context.Context) (int, error) {
	if err := c.waitForReconcile(ctx); err != nil {
		return 0, err
	}
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
	c.log.Info().Int("tracked", len(ids)).Msg("intraday: populating redis with today's candles")

	idCh := make(chan int64)
	var processed, cached int32
	var wg sync.WaitGroup
	for w := 0; w < refreshWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range idCh {
				if c.refreshOne(ctx, id) {
					atomic.AddInt32(&cached, 1)
				}
				if p := atomic.AddInt32(&processed, 1); p%10 == 0 {
					c.log.Info().Int32("processed", p).Int("tracked", len(ids)).Int32("cached", atomic.LoadInt32(&cached)).Msg("intraday: redis populate progress")
				}
			}
		}()
	}
	for _, id := range ids {
		idCh <- id
	}
	close(idCh)
	wg.Wait()

	c.rdb.Set(ctx, builtKey, time.Now().In(market.IST).Format(time.RFC3339), c.ttl)
	c.log.Info().Int32("cached", cached).Int("tracked", len(ids)).Msg("intraday: cache refreshed")
	return int(cached), nil
}

// refreshOne fetches and caches today's candle for one instrument. It reports
// whether the cache was updated; on any error or missing candle, the previous
// cached value (if any) is left in place.
func (c *Cache) refreshOne(ctx context.Context, id int64) bool {
	inst, err := c.inst.GetByID(ctx, id)
	if err != nil {
		return false
	}
	cd, found, err := c.fetchToday(ctx, inst)
	if err != nil {
		if isRateLimitErr(err) {
			c.log.Warn().Err(err).Int64("instrument", id).Str("symbol", inst.TradingSymbol).Msg("intraday: rate limited by Angel; keeping previous cached value")
		} else {
			c.log.Debug().Err(err).Int64("instrument", id).Str("symbol", inst.TradingSymbol).Msg("intraday: fetchToday errored; keeping previous cached value")
		}
		return false
	}
	if !found {
		c.log.Debug().Int64("instrument", id).Str("symbol", inst.TradingSymbol).Msg("intraday: no candle yet (no trades today); keeping previous cached value")
		return false
	}
	b, _ := json.Marshal(cd)
	return c.rdb.Set(ctx, key(id), b, c.ttl).Err() == nil
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
