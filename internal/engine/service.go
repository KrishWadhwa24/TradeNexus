// Package engine orchestrates the scan pipeline: load/fetch candles, run the
// scanner engine, persist signals (audit), and reconcile missing data. It's the
// glue between the pure scanner logic and the datastores/Angel client.
package engine

import (
	"context"
	"strings"
	"time"

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
	candles   *candles.Repo
	signals   *signals.Repo
	inst      *instruments.Repo
	angel     *angel.Client
	cal       *calendar.Service
	notifier  *notify.Dispatcher // optional; nil disables notifications
	pineCfg   scanner.PineConfig
	retention time.Duration
	log       zerolog.Logger
}

// New builds the engine service. notifier may be nil to disable fan-out.
func New(c *candles.Repo, sig *signals.Repo, inst *instruments.Repo, ang *angel.Client,
	cal *calendar.Service, notifier *notify.Dispatcher, pineCfg scanner.PineConfig,
	retention time.Duration, log zerolog.Logger) *Service {
	return &Service{
		candles: c, signals: sig, inst: inst, angel: ang, cal: cal,
		notifier: notifier, pineCfg: pineCfg, retention: retention, log: log,
	}
}

// ScanResult summarizes a single instrument scan.
type ScanResult struct {
	InstrumentID    int64          `json:"instrument_id"`
	Report          scanner.Report `json:"report"`
	SignalsInserted int            `json:"signals_inserted"`
}

// ScanStored runs the scanner on already-stored candles and persists signals.
func (s *Service) ScanStored(ctx context.Context, instrumentID int64) (ScanResult, error) {
	daily, err := s.candles.GetDaily(ctx, instrumentID)
	if err != nil {
		return ScanResult{}, err
	}
	weeklyAgg, err := s.candles.GetAggregates(ctx, instrumentID, market.TF1W)
	if err != nil {
		return ScanResult{}, err
	}
	monthlyAgg, err := s.candles.GetAggregates(ctx, instrumentID, market.TF1M)
	if err != nil {
		return ScanResult{}, err
	}

	weekly := market.AggToCandles(weeklyAgg)
	monthly := market.AggToCandles(monthlyAgg)
	report := scanner.Run(daily, weekly, monthly, s.pineCfg)

	inserted, err := s.persist(ctx, instrumentID, report, daily, weekly, monthly)
	if err != nil {
		return ScanResult{}, err
	}
	return ScanResult{InstrumentID: instrumentID, Report: report, SignalsInserted: inserted}, nil
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

// SyncAndScan fetches `days` of daily history from Angel, stores it, rebuilds
// aggregates, and scans.
func (s *Service) SyncAndScan(ctx context.Context, instrumentID, days int) (ScanResult, error) {
	inst, err := s.inst.GetByID(ctx, int64(instrumentID))
	if err != nil {
		return ScanResult{}, err
	}
	if days <= 0 {
		days = candles.RequiredDailyBars
	}
	if days > 2000 {
		days = 2000
	}
	to := time.Now().In(market.IST)
	from := to.AddDate(0, 0, -(days*7/5 + 10))

	fetched, err := s.angel.GetDailyCandles(ctx, inst.Exchange, inst.SymbolToken, from, to)
	if err != nil {
		return ScanResult{}, err
	}
	if _, err := s.candles.UpsertDaily(ctx, int64(instrumentID), fetched); err != nil {
		return ScanResult{}, err
	}
	if _, _, err := s.candles.RebuildAggregates(ctx, int64(instrumentID)); err != nil {
		return ScanResult{}, err
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
