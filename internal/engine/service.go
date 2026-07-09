// Package engine orchestrates the scan pipeline: load/fetch candles, run the
// scanner engine, persist signals (audit), and reconcile missing data. It's the
// glue between the pure scanner logic and the datastores/Angel client.
package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"tradenexus/internal/angel"
	"tradenexus/internal/calendar"
	"tradenexus/internal/candles"
	"tradenexus/internal/instruments"
	"tradenexus/internal/market"
	"tradenexus/internal/notify"
	"tradenexus/internal/scanner"
	"tradenexus/internal/signals"
)

// Service coordinates scanning and reconciliation.
type Service struct {
	candles   Candler
	signals   Signaler
	inst      *instruments.Repo // Assuming not all methods are needed for an interface yet
	angel     *angel.Client     // Assuming not all methods are needed for an interface yet
	cal       CalendarProvider  // Use an interface for testability
	notifier  *notify.Dispatcher
	redis     Redis
	pineCfg   scanner.PineConfig
	retention time.Duration
	log       zerolog.Logger
}

// CalendarProvider defines the interface for calendar operations needed by the engine.
type CalendarProvider interface {
	IsMarketOpen(time.Time) bool
	Cal() *calendar.Calendar
}

// New builds the engine service. notifier may be nil to disable fan-out.
func New(c Candler, sig Signaler, inst *instruments.Repo, ang *angel.Client,
	cal CalendarProvider, notifier *notify.Dispatcher, redis Redis, pineCfg scanner.PineConfig,
	retention time.Duration, log zerolog.Logger) *Service {
	return &Service{
		candles:   c,
		signals:   sig,
		inst:      inst,
		angel:     ang,
		cal:       cal,
		notifier:  notifier,
		redis:     redis,
		pineCfg:   pineCfg,
		retention: retention,
		log:       log,
	}
}

// ScanResult summarizes a single instrument scan.
type ScanResult struct {
	InstrumentID    int64          `json:"instrument_id"`
	Report          scanner.Report `json:"report"`
	SignalsInserted int            `json:"signals_inserted"`
}

// ScanStored runs the scanner on already-stored candles and persists signals.
// During market hours, it prioritizes fetching today's candle from Redis and
// merging it with historical data from the database. After market hours, it
// bypasses Redis and uses only the database.
func (s *Service) ScanStored(ctx context.Context, instrumentID int64) (ScanResult, error) {
	// Before proceeding, wait for any active cache population to finish.
	if s.cal.IsMarketOpen(time.Now()) {
		if err := s.waitForCache(ctx); err != nil {
			return ScanResult{}, fmt.Errorf("failed while waiting for cache: %w", err)
		}
	}

	var (
		daily   []market.Candle
		weekly  []market.Candle
		monthly []market.Candle
		err     error
	)

	if s.cal.IsMarketOpen(time.Now()) {
		// Market is OPEN: Use Redis for today's candle + DB for history.
		s.log.Debug().Int64("instrument_id", instrumentID).Msg("market open, scanning with intraday cache")

		// 1. Get historical data from the database.
		dbCandles, err := s.candles.GetDaily(ctx, instrumentID)
		if err != nil {
			return ScanResult{}, fmt.Errorf("failed to get db candles: %w", err)
		}

		// 2. Try to get today's candle from Redis.
		cachedData, err := s.redis.GetCachedCandles(ctx, instrumentID)
		if err != nil && err != redis.Nil {
			s.log.Warn().Err(err).Int64("instrument_id", instrumentID).Msg("failed to get cached candles")
		}

		var intraDayCandle []market.Candle
		if len(cachedData) > 0 {
			s.log.Debug().Int64("instrument_id", instrumentID).Msg("using cached intraday candle")
			if err := json.Unmarshal(cachedData, &intraDayCandle); err != nil {
				s.log.Error().Err(err).Int64("instrument_id", instrumentID).Msg("failed to unmarshal cached intraday candle")
				// Don't fail the whole scan, just proceed with DB data.
			}
		} else {
			s.log.Debug().Int64("instrument_id", instrumentID).Msg("intraday cache miss")
		}

		// 3. Merge historical and intraday data.
		daily = mergeCandles(dbCandles, intraDayCandle)

		// 4. Aggregates are not stored directly, so we derive them from the merged daily data.
		weeklyAgg := candles.Weekly(daily)
		weekly = market.AggToCandles(weeklyAgg)
		monthlyAgg := candles.Monthly(daily)
		monthly = market.AggToCandles(monthlyAgg)

	} else {
		// Market is CLOSED: Use database only.
		daily, err = s.candles.GetDaily(ctx, instrumentID)
		if err != nil {
			return ScanResult{}, err
		}
		s.log.Debug().Int64("instrument_id", instrumentID).Int("candle_count", len(daily)).Msg("market closed, scanning from database only")

		// Also fetch aggregates from DB.
		weeklyAgg, err := s.candles.GetAggregates(ctx, instrumentID, market.TF1W)
		if err != nil {
			return ScanResult{}, err
		}
		monthlyAgg, err := s.candles.GetAggregates(ctx, instrumentID, market.TF1M)
		if err != nil {
			return ScanResult{}, err
		}
		weekly = market.AggToCandles(weeklyAgg)
		monthly = market.AggToCandles(monthlyAgg)
	}

	// With data now loaded, run the scanner.
	report := scanner.Run(daily, weekly, monthly, s.pineCfg)

	inserted, err := s.persist(ctx, instrumentID, report, daily, weekly, monthly)
	if err != nil {
		return ScanResult{}, err
	}
	return ScanResult{InstrumentID: instrumentID, Report: report, SignalsInserted: inserted}, nil
}

// mergeCandles combines two slices of candles. Candles from the `new` slice
// overwrite candles from the `old` slice for the same date. The final slice is
// sorted.
func mergeCandles(old, new []market.Candle) []market.Candle {
	// Use a map for efficient lookup and replacement of candles.
	merged := make(map[time.Time]market.Candle)
	for _, c := range old {
		merged[c.Time.Truncate(24 * time.Hour)] = c
	}
	for _, c := range new {
		merged[c.Time.Truncate(24 * time.Hour)] = c
	}

	// Convert map back to slice.
	result := make([]market.Candle, 0, len(merged))
	for _, c := range merged {
		result = append(result, c)
	}

	// Sort by date to ensure the series is correct.
	sort.Slice(result, func(i, j int) bool {
		return result[i].Time.Before(result[j].Time)
	})

	return result
}

// persist writes any fired signals to the audit store (idempotent).
func (s *Service) persist(ctx context.Context, instID int64, rep scanner.Report,
	daily, weekly, monthly []market.Candle) (int, error) {
	n := 0
	add := func(sig signals.Signal) error {
		ins, id, err := s.signals.Upsert(ctx, sig)
		if err != nil {
			return err
		}
		if ins {
			n++
			// Fan out newly generated signals (window + dedup handled inside).
			if s.notifier != nil {
				sig.ID = id
				if _, derr := s.notifier.Dispatch(ctx, sig); derr != nil {
					s.log.Error().Err(derr).Int64("signal", id).Msg("dispatch failed")
				}
			}
		}
		return nil
	}

	// Daily Pine (closed candle only — the last daily bar is already closed).
	if len(daily) > 0 && (rep.DailyPine.Buy || rep.DailyPine.Sell) {
		if err := add(pineSignal(instID, market.TF1D, lastTime(daily), rep.DailyPine)); err != nil {
			return n, err
		}
	}
	// Weekly Pine (forming bar allowed).
	if len(weekly) > 0 && (rep.WeeklyPine.Buy || rep.WeeklyPine.Sell) {
		if err := add(pineSignal(instID, market.TF1W, lastTime(weekly), rep.WeeklyPine)); err != nil {
			return n, err
		}
	}
	// Monthly Pine (forming bar allowed).
	if len(monthly) > 0 && (rep.MonthlyPine.Buy || rep.MonthlyPine.Sell) {
		if err := add(pineSignal(instID, market.TF1M, lastTime(monthly), rep.MonthlyPine)); err != nil {
			return n, err
		}
	}
	// Weekly scanners: fire when >= 1 of 4 (confidence = N/4).
	if len(weekly) > 0 && rep.Weekly.Confidence >= 1 {
		conf := rep.Weekly.Confidence
		if err := add(signals.Signal{
			InstrumentID: instID,
			Source:       "weekly",
			ScannerName:  strings.Join(rep.Weekly.Fired, ","),
			Timeframe:    market.TF1W,
			Direction:    "BUY",
			CandleDate:   lastTime(weekly),
			Confidence:   &conf,
			RSI:          rep.Weekly.RSI,
			Volume:       rep.Weekly.Volume,
			Reasons:      rep.Weekly.Details,
		}); err != nil {
			return n, err
		}
	}
	if err := s.persistPatternSignals(add, instID, market.TF1D, lastTimeOrZero(daily), rep.Patterns.Daily); err != nil {
		return n, err
	}
	if err := s.persistPatternSignals(add, instID, market.TF1W, lastTimeOrZero(weekly), rep.Patterns.Weekly); err != nil {
		return n, err
	}
	if err := s.persistPatternSignals(add, instID, market.TF1M, lastTimeOrZero(monthly), rep.Patterns.Monthly); err != nil {
		return n, err
	}
	return n, nil
}

func (s *Service) persistPatternSignals(add func(signals.Signal) error, instID int64, tf string, date time.Time, res scanner.PatternTimeframeResult) error {
	if date.IsZero() {
		return nil
	}
	patterns := []struct {
		name string
		sig  scanner.PatternSignal
	}{
		{scanner.PatternDowntrendBreakout, res.DowntrendBreakout},
		{scanner.PatternRectangle, res.Rectangle},
		{scanner.PatternCupHandle, res.CupHandle},
	}
	for _, p := range patterns {
		if !p.sig.Buy {
			continue
		}
		if err := add(signals.Signal{
			InstrumentID: instID,
			Source:       "patterns",
			ScannerName:  p.name,
			Timeframe:    tf,
			Direction:    "BUY",
			CandleDate:   date,
			Reasons:      p.sig.Reasons,
		}); err != nil {
			return err
		}
	}
	return nil
}

func pineSignal(instID int64, tf string, date time.Time, sig scanner.PineSignal) signals.Signal {
	dir := "BUY"
	if sig.Sell {
		dir = "SELL"
	}
	return signals.Signal{
		InstrumentID: instID,
		Source:       "pine",
		ScannerName:  "pine",
		Timeframe:    tf,
		Direction:    dir,
		CandleDate:   date,
		Reasons:      sig.Reasons,
	}
}

func lastTime(c []market.Candle) time.Time { return c[len(c)-1].Time }

func lastTimeOrZero(c []market.Candle) time.Time {
	if len(c) == 0 {
		return time.Time{}
	}
	return lastTime(c)
}

// SyncAndScan intelligently fetches missing daily history from Angel, stores it,
// rebuilds aggregates, and scans. It's the efficient engine for the "Scan Now"
// button.
func (s *Service) SyncAndScan(ctx context.Context, instrumentID, days int) (ScanResult, error) {
	inst, err := s.inst.GetByID(ctx, int64(instrumentID))
	if err != nil {
		return ScanResult{}, err
	}

	// Check what data we already have.
	set, first, _, ok, err := s.candles.DailyDateSet(ctx, int64(instrumentID))
	if err != nil {
		return ScanResult{}, err
	}

	to := time.Now().In(market.IST)
	var from time.Time

	// If no data exists, bootstrap the full history.
	if !ok {
		if days <= 0 {
			days = candles.RequiredDailyBars
		}
		if days > 2000 {
			days = 2000
		}
		from = to.AddDate(0, 0, -(days*7/5 + 10))
	} else {
		// Data exists, find what's missing.
		missing := s.cal.Cal().MissingTradingDays(first.AddDate(0, 0, -1), to, set)
		if len(missing) > 0 {
			from = missing[0] // Fetch from the first missing day.
		} else {
			from = to // Nothing's missing, just fetch today's update.
		}
	}

	fetched, err := s.angel.GetDailyCandles(ctx, inst.Exchange, inst.SymbolToken, from, to)
	if err != nil {
		return ScanResult{}, err
	}

	// Only proceed if we actually got new data.
	if len(fetched) > 0 {
		if _, err := s.candles.UpsertDaily(ctx, int64(instrumentID), fetched); err != nil {
			return ScanResult{}, err
		}
		if _, _, err := s.candles.RebuildAggregates(ctx, int64(instrumentID)); err != nil {
			return ScanResult{}, err
		}
	}

	return s.ScanStored(ctx, int64(instrumentID))
}

// ReconcileResult summarizes a reconciliation pass.
type ReconcileResult struct {
	InstrumentID    int64 `json:"instrument_id"`
	MissingDays     int   `json:"missing_days"`
	Fetched         int   `json:"fetched"`
	SignalsInserted int   `json:"signals_inserted"`
	Bootstrapped    bool  `json:"bootstrapped"`
}

// Reconcile detects and backfills missing daily candles (distinguishing gaps
// from weekends/holidays), rebuilds aggregates, and re-scans. Runs on startup,
// after downtime, and each trading day.
func (s *Service) Reconcile(ctx context.Context, instrumentID int64) (ReconcileResult, error) {
	inst, err := s.inst.GetByID(ctx, instrumentID)
	if err != nil {
		return ReconcileResult{}, err
	}
	set, first, _, ok, err := s.candles.DailyDateSet(ctx, instrumentID)
	if err != nil {
		return ReconcileResult{}, err
	}
	// No data at all → full history bootstrap.
	if !ok {
		res, err := s.SyncAndScan(ctx, int(instrumentID), candles.RequiredDailyBars)
		if err != nil {
			return ReconcileResult{}, err
		}
		return ReconcileResult{InstrumentID: instrumentID, Fetched: -1, SignalsInserted: res.SignalsInserted, Bootstrapped: true}, nil
	}

	today := time.Now().In(market.IST)
	missing := s.cal.Cal().MissingTradingDays(first.AddDate(0, 0, -1), today, set)

	// Nothing missing — still re-scan so the forming weekly/monthly bar updates.
	if len(missing) == 0 {
		res, err := s.ScanStored(ctx, instrumentID)
		if err != nil {
			return ReconcileResult{}, err
		}
		return ReconcileResult{InstrumentID: instrumentID, SignalsInserted: res.SignalsInserted}, nil
	}

	from := missing[0]
	fetched, err := s.angel.GetDailyCandles(ctx, inst.Exchange, inst.SymbolToken, from, today)
	if err != nil {
		return ReconcileResult{}, err
	}
	if _, err := s.candles.UpsertDaily(ctx, instrumentID, fetched); err != nil {
		return ReconcileResult{}, err
	}
	if _, _, err := s.candles.RebuildAggregates(ctx, instrumentID); err != nil {
		return ReconcileResult{}, err
	}
	res, err := s.ScanStored(ctx, instrumentID)
	if err != nil {
		return ReconcileResult{}, err
	}
	return ReconcileResult{
		InstrumentID:    instrumentID,
		MissingDays:     len(missing),
		Fetched:         len(fetched),
		SignalsInserted: res.SignalsInserted,
	}, nil
}

// ScanAll scans every tracked instrument from stored candles (no Angel calls).
func (s *Service) ScanAll(ctx context.Context) ([]ScanResult, error) {
	ids, err := s.candles.ListInstrumentIDsWithData(ctx)
	if err != nil {
		return nil, err
	}
	var out []ScanResult
	for _, id := range ids {
		r, err := s.ScanStored(ctx, id)
		if err != nil {
			s.log.Error().Err(err).Int64("instrument", id).Msg("scan-all: instrument failed")
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// ReconcileAll reconciles + scans every tracked instrument. This is the daily
// job and the startup-recovery routine.
func (s *Service) ReconcileAll(ctx context.Context) ([]ReconcileResult, error) {
	ids, err := s.candles.ListInstrumentIDsWithData(ctx)
	if err != nil {
		return nil, err
	}
	var out []ReconcileResult
	for _, id := range ids {
		r, err := s.Reconcile(ctx, id)
		if err != nil {
			s.log.Error().Err(err).Int64("instrument", id).Msg("reconcile-all: instrument failed")
			continue
		}
		out = append(out, r)

	}
	return out, nil
}

// Cleanup removes signals older than the retention window (30 days by default).
func (s *Service) Cleanup(ctx context.Context) (int64, error) {
	return s.signals.DeleteOlderThan(ctx, s.retention)
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// waitForCache polls Redis to see if the cache is being populated and waits
// until the process is complete or a timeout is reached.
func (s *Service) waitForCache(ctx context.Context) error {
	const (
		pollInterval = 500 * time.Millisecond
		maxWait      = 2 * time.Minute // Don't wait forever.
	)

	waitCtx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()

	for {
		populating, err := s.redis.IsCachePopulating(waitCtx)
		if err != nil {
			return fmt.Errorf("could not check cache status: %w", err)
		}
		if !populating {
			return nil // Cache is ready.
		}

		s.log.Debug().Msg("cache is being populated, waiting...")
		select {
		case <-time.After(pollInterval):
			// Continue loop.
		case <-waitCtx.Done():
			return fmt.Errorf("timed out waiting for cache to be ready: %w", waitCtx.Err())
		}
	}
}

// GetCachedScanResult retrieves a scan result from the cache.
func (s *Service) GetCachedScanResult(ctx context.Context, key string) (*ScanResult, error) {
	data, err := s.redis.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	var result ScanResult
	if err := json.Unmarshal(data, &result); err != nil {
		s.log.Error().Err(err).Str("key", key).Bytes("data", data).Msg("failed to unmarshal cached scan result")
		return nil, fmt.Errorf("unmarshal scan result: %w", err)
	}
	return &result, nil
}

// CacheScanResult stores a scan result in the cache. It sanitizes NaN values
// to nulls to ensure JSON compatibility.
func (s *Service) CacheScanResult(ctx context.Context, key string, result ScanResult, ttl time.Duration) error {
	b, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal scan result: %w", err)
	}
	// NaN is not valid in standard JSON. Replace it with null.
	b = bytes.ReplaceAll(b, []byte("NaN"), []byte("null"))
	return s.redis.SetBytes(ctx, key, b, ttl)
}
