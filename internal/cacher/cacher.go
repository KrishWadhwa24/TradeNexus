// Package cacher provides a background service that proactively caches daily
// candle data for all tracked instruments to reduce load on the Angel API and
// speed up user-facing scans.
package cacher

import (
	"context"
	"encoding/json"
	"time"

	"github.com/rs/zerolog"

	"tradenexus/internal/config"
	"tradenexus/internal/instruments"
	"tradenexus/internal/market"
)

// MarketStatusChecker defines the interface for checking if the market is open.
type MarketStatusChecker interface {
	IsMarketOpen(time.Time) bool
}

// AngelClient defines the interface for fetching candle data.
type AngelClient interface {
	GetDailyCandles(ctx context.Context, exchange, token string, from, to time.Time) ([]market.Candle, error)
}

// Redis defines the interface for Redis operations needed by the cacher.
type Redis interface {
	SetCachedCandles(ctx context.Context, instrumentID int64, data []byte, ttl time.Duration) error
	SetCachePopulating(ctx context.Context, isPopulating bool, ttl time.Duration) error
}

// CandleRepo defines the interface for candle repository operations.
type CandleRepo interface {
	ListInstrumentIDsWithData(context.Context) ([]int64, error)
}

// InstrumentRepo defines the interface for instrument repository operations.
type InstrumentRepo interface {
	GetByID(ctx context.Context, id int64) (instruments.Instrument, error)
}

// Cacher is the background caching service.
type Cacher struct {
	cfg     config.Config
	log     zerolog.Logger
	inst    InstrumentRepo
	candles CandleRepo
	angel   AngelClient
	redis   Redis
	cal     MarketStatusChecker
	stop    chan struct{}
}

// New creates a new Cacher service.
func New(cfg config.Config, log zerolog.Logger, inst InstrumentRepo, candles CandleRepo, angel AngelClient, redis Redis, cal MarketStatusChecker) *Cacher {
	return &Cacher{
		cfg:     cfg,
		log:     log.With().Str("service", "cacher").Logger(),
		inst:    inst,
		candles: candles,
		angel:   angel,
		redis:   redis,
		cal:     cal,
		stop:    make(chan struct{}),
	}
}

// Start begins the background caching loop.
func (c *Cacher) Start() {
	if !c.cfg.CacheEnabled {
		c.log.Info().Msg("caching service disabled")
		return
	}

	c.log.Info().
		Dur("interval", c.cfg.CacheInterval).
		Dur("ttl", c.cfg.CacheTTL).
		Msg("starting caching service")

	ticker := time.NewTicker(c.cfg.CacheInterval)
	go func() {
		for {
			select {
			case <-ticker.C:
				if c.cal.IsMarketOpen(time.Now()) {
					c.populateCache()
				} else {
					c.log.Debug().Msg("market closed, skipping cache population")
				}
			case <-c.stop:
				ticker.Stop()
				return
			}
		}
	}()
}

// Stop halts the background caching loop.
func (c *Cacher) Stop() {
	if c.stop != nil {
		close(c.stop)
	}
}

func (c *Cacher) populateCache() {
	c.log.Info().Msg("starting cache population run")
	ctx := context.Background()

	// Set a flag indicating the cache is being populated.
	// The TTL should be long enough to cover the entire run.
	// 400 stocks * (300ms delay + avg fetch time) + buffer
	runTTL := time.Duration(c.cfg.CacheRetryAmnt+1) * time.Minute * 5 // A generous TTL.
	if err := c.redis.SetCachePopulating(ctx, true, runTTL); err != nil {
		c.log.Error().Err(err).Msg("failed to set cache populating flag")
		return
	}
	// Ensure the flag is cleared on completion or panic.
	defer func() {
		if err := c.redis.SetCachePopulating(ctx, false, 0); err != nil {
			c.log.Error().Err(err).Msg("failed to clear cache populating flag")
		}
		c.log.Info().Msg("cache population run finished")
	}()

	ids, err := c.candles.ListInstrumentIDsWithData(ctx)
	if err != nil {
		c.log.Error().Err(err).Msg("failed to list tracked instruments")
		return
	}

	c.log.Info().Int("count", len(ids)).Msg("found tracked instruments to cache")

	for _, id := range ids {
		c.fetchAndCacheWithRetry(ctx, id)
		// Pause between each instrument to distribute the load.
		time.Sleep(c.cfg.CacheRequestDelay)
	}
}

func (c *Cacher) fetchAndCacheWithRetry(ctx context.Context, instrumentID int64) {
	var (
		apiCandles []market.Candle
		err        error
	)

	inst, err := c.inst.GetByID(ctx, instrumentID)
	if err != nil {
		c.log.Error().Err(err).Int64("instrument_id", instrumentID).Msg("failed to get instrument details")
		return
	}

	// 1. Fetch last 2 days from Angel to get today's incomplete candle.
	for i := 0; i < c.cfg.CacheRetryAmnt; i++ {
		// Fetch from yesterday to today.
		from := time.Now().AddDate(0, 0, -1)
		to := time.Now()
		apiCandles, err = c.angel.GetDailyCandles(ctx, inst.Exchange, inst.SymbolToken, from, to)
		if err == nil {
			break // Success
		}
		c.log.Warn().
			Err(err).
			Int64("instrument_id", instrumentID).
			Int("attempt", i+1).
			Msg("failed to fetch recent candles, retrying...")
		time.Sleep(c.cfg.CacheRetryDelay)
	}

	if err != nil {
		c.log.Error().
			Err(err).
			Int64("instrument_id", instrumentID).
			Int("attempts", c.cfg.CacheRetryAmnt).
			Msg("failed to fetch recent candles after all retries")
		return
	}

	// 2. Find today's candle from the response.
	var todaysCandle *market.Candle
	todayDateStr := time.Now().Format("2006-01-02")
	for i := range apiCandles {
		if apiCandles[i].Time.Format("2006-01-02") == todayDateStr {
			todaysCandle = &apiCandles[i]
			break
		}
	}

	if todaysCandle == nil {
		c.log.Warn().Int64("instrument_id", instrumentID).Msg("could not find today's candle in API response")
		return
	}

	// 3. Store only today's candle in Redis.
	// We store it as a slice with one element to maintain compatibility with
	// the existing Redis function, which expects []byte from []market.Candle.
	// This will be handled properly in the engine service.
	data, err := json.Marshal([]market.Candle{*todaysCandle})
	if err != nil {
		c.log.Error().Err(err).Int64("instrument_id", instrumentID).Msg("failed to marshal today's candle to JSON")
		return
	}

	if err := c.redis.SetCachedCandles(ctx, instrumentID, data, c.cfg.CacheTTL); err != nil {
		c.log.Error().Err(err).Int64("instrument_id", instrumentID).Msg("failed to set today's candle in redis cache")
		return
	}

	c.log.Debug().Int64("instrument_id", instrumentID).Msg("cached today's instrument candle")
}
