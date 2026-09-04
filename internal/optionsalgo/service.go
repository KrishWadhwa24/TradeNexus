package optionsalgo

import (
	"context"
	"time"

	"github.com/rs/zerolog"

	"tradenexus/internal/angel"
	"tradenexus/internal/calendar"
	"tradenexus/internal/cronx"
	"tradenexus/internal/market"
)

// backfillWindow is intentionally wide — verified live against Angel that
// ONE_MINUTE requests silently cap at ~7,987 bars (~21 trading days)
// regardless of how far back `from` asks, always ending at `to`. Asking for
// more than that costs nothing extra (same response either way), so this
// just has to be "at least as wide as Angel's real cap," not tuned exactly
// to it.
const backfillWindow = 30 * 24 * time.Hour

// refreshInterval is how often the live-refresh tick runs during market
// hours — far more frequent than the daily equity scan, since this feeds a
// strategy engine reacting intraday, not an end-of-day scan.
const refreshInterval = 1 * time.Minute

// Service maintains 1-minute candle history for the options-algo underlying
// instruments (Nifty 50, SENSEX) — entirely separate from the equity
// scan/reconcile pipeline (internal/engine), which never calls anything
// here and is untouched by it.
type Service struct {
	angel *angel.Client
	repo  *Repo
	cal   *calendar.Service
	log   zerolog.Logger
}

// New builds the service.
func New(angelClient *angel.Client, repo *Repo, cal *calendar.Service, log zerolog.Logger) *Service {
	return &Service{angel: angelClient, repo: repo, cal: cal, log: log}
}

// Backfill seeds minute_candles for every tracked underlying. Safe to
// re-run — UpsertMinuteCandles overwrites, and re-fetching the same ~21-day
// window Angel already gave us is a cheap no-op in practice, not a growing
// cost.
func (s *Service) Backfill(ctx context.Context) error {
	underlyings, err := s.repo.TrackedUnderlyings(ctx)
	if err != nil {
		return err
	}
	now := time.Now().In(market.IST)
	for _, u := range underlyings {
		bars, err := s.angel.GetIntradayCandles(ctx, u.Exchange, u.SymbolToken, angel.IntervalOneMinute, now.Add(-backfillWindow), now)
		if err != nil {
			s.log.Error().Err(err).Str("symbol", u.TradingSymbol).Msg("optionsalgo: backfill fetch failed")
			continue
		}
		n, err := s.repo.UpsertMinuteCandles(ctx, u.ID, bars)
		if err != nil {
			s.log.Error().Err(err).Str("symbol", u.TradingSymbol).Msg("optionsalgo: backfill store failed")
			continue
		}
		s.log.Info().Str("symbol", u.TradingSymbol).Int("bars", n).Msg("optionsalgo: backfill done")
	}
	return nil
}

// epochYear is the year Postgres' 'epoch'::timestamptz (used by
// LatestCandleTime as its "no rows yet" sentinel) falls in — used here to
// tell "never backfilled" apart from a real stored timestamp without a
// second nullable-time dance.
const epochYear = 1970

// RefreshLatest fetches only bars newer than each underlying's last stored
// candle and appends them — the routine market-hours tick, far cheaper than
// re-running the full backfill window every time.
func (s *Service) RefreshLatest(ctx context.Context) error {
	underlyings, err := s.repo.TrackedUnderlyings(ctx)
	if err != nil {
		return err
	}
	now := time.Now().In(market.IST)
	for _, u := range underlyings {
		latest, err := s.repo.LatestCandleTime(ctx, u.ID)
		if err != nil {
			s.log.Error().Err(err).Str("symbol", u.TradingSymbol).Msg("optionsalgo: refresh latest-time lookup failed")
			continue
		}
		from := latest
		if latest.Year() <= epochYear {
			from = now.Add(-backfillWindow) // never backfilled — fall back to a full pull
		}
		bars, err := s.angel.GetIntradayCandles(ctx, u.Exchange, u.SymbolToken, angel.IntervalOneMinute, from, now)
		if err != nil {
			s.log.Error().Err(err).Str("symbol", u.TradingSymbol).Msg("optionsalgo: refresh fetch failed")
			continue
		}
		if _, err := s.repo.UpsertMinuteCandles(ctx, u.ID, bars); err != nil {
			s.log.Error().Err(err).Str("symbol", u.TradingSymbol).Msg("optionsalgo: refresh store failed")
		}
	}
	return nil
}

// StartPolling runs an immediate backfill, then refreshes every
// refreshInterval while the market is open, until ctx is cancelled.
func (s *Service) StartPolling(ctx context.Context) {
	go cronx.Safe(s.log, func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := s.Backfill(c); err != nil {
			s.log.Error().Err(err).Msg("optionsalgo: initial backfill failed")
		}
	})
	go func() {
		t := time.NewTicker(refreshInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cronx.Safe(s.log, func() {
					if !s.cal.Cal().IsMarketOpen(time.Now().In(market.IST)) {
						return
					}
					c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
					defer cancel()
					if err := s.RefreshLatest(c); err != nil {
						s.log.Error().Err(err).Msg("optionsalgo: refresh failed")
					}
				})
			}
		}
	}()
}
