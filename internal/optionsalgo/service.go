package optionsalgo

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"tradenexus/internal/angel"
	"tradenexus/internal/calendar"
	"tradenexus/internal/cronx"
	"tradenexus/internal/instruments"
	"tradenexus/internal/live"
	"tradenexus/internal/market"
	"tradenexus/internal/paper"
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
	// paper is used only by the execution bridge (execute.go) — placing and
	// managing the actual paper trades. Building/refreshing candles above
	// never touches it. May be nil in tests that only exercise the candle
	// pipeline.
	paper *paper.Service
	// live is the shared Angel websocket hub — used to keep the option
	// chain's bid/ask/volume/OI streaming continuously instead of polling
	// REST every cycle (see chain_live_ws.go). May be nil in tests that
	// don't exercise the chain; BuildOptionChain falls back to REST-only
	// when nil.
	live *live.Hub

	chainSubMu     sync.Mutex
	chainSubCancel func()
	chainSubKeys   map[string]bool
}

// New builds the service.
func New(angelClient *angel.Client, repo *Repo, cal *calendar.Service, paperSvc *paper.Service, liveHub *live.Hub, log zerolog.Logger) *Service {
	return &Service{angel: angelClient, repo: repo, cal: cal, paper: paperSvc, live: liveHub, log: log}
}

// backfillOne pulls the full backfillWindow of 1-minute bars for one
// instrument and upserts them. Shared by Backfill (underlyings) and
// BackfillFutures — identical logic either way, only which instrument list
// calls it differs, so behavior for the existing underlyings is byte-for-byte
// unchanged by adding futures support.
func (s *Service) backfillOne(ctx context.Context, u instruments.Instrument, now time.Time) {
	bars, err := s.angel.GetIntradayCandles(ctx, u.Exchange, u.SymbolToken, angel.IntervalOneMinute, now.Add(-backfillWindow), now)
	if err != nil {
		s.log.Error().Err(err).Str("symbol", u.TradingSymbol).Msg("optionsalgo: backfill fetch failed")
		return
	}
	n, err := s.repo.UpsertMinuteCandles(ctx, u.ID, bars)
	if err != nil {
		s.log.Error().Err(err).Str("symbol", u.TradingSymbol).Msg("optionsalgo: backfill store failed")
		return
	}
	s.log.Info().Str("symbol", u.TradingSymbol).Int("bars", n).Msg("optionsalgo: backfill done")
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
		s.backfillOne(ctx, u, now)
	}
	return nil
}

// BackfillFutures seeds minute_candles for the tracked NIFTY future (see
// Repo.TrackedFutures) — tracked only as a real-volume VWAP proxy, not for
// trading. Separate entry point from Backfill, sharing only backfillOne's
// fetch/upsert logic, so Backfill's existing behavior for Nifty 50/SENSEX is
// untouched by this.
func (s *Service) BackfillFutures(ctx context.Context) error {
	futures, err := s.repo.TrackedFutures(ctx)
	if err != nil {
		return err
	}
	now := time.Now().In(market.IST)
	for _, f := range futures {
		s.backfillOne(ctx, f, now)
	}
	return nil
}

// epochYear is the year Postgres' 'epoch'::timestamptz (used by
// LatestCandleTime as its "no rows yet" sentinel) falls in — used here to
// tell "never backfilled" apart from a real stored timestamp without a
// second nullable-time dance.
const epochYear = 1970

// refreshOne fetches only bars newer than one instrument's last stored candle
// and appends them. Shared by RefreshLatest (underlyings) and
// RefreshFuturesLatest — same note as backfillOne: factored out, not
// rewritten, so RefreshLatest's existing behavior is unchanged.
func (s *Service) refreshOne(ctx context.Context, u instruments.Instrument, now time.Time) {
	latest, err := s.repo.LatestCandleTime(ctx, u.ID)
	if err != nil {
		s.log.Error().Err(err).Str("symbol", u.TradingSymbol).Msg("optionsalgo: refresh latest-time lookup failed")
		return
	}
	from := latest
	if latest.Year() <= epochYear {
		from = now.Add(-backfillWindow) // never backfilled — fall back to a full pull
	}
	bars, err := s.angel.GetIntradayCandles(ctx, u.Exchange, u.SymbolToken, angel.IntervalOneMinute, from, now)
	if err != nil {
		s.log.Error().Err(err).Str("symbol", u.TradingSymbol).Msg("optionsalgo: refresh fetch failed")
		return
	}
	if _, err := s.repo.UpsertMinuteCandles(ctx, u.ID, bars); err != nil {
		s.log.Error().Err(err).Str("symbol", u.TradingSymbol).Msg("optionsalgo: refresh store failed")
	}
}

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
		s.refreshOne(ctx, u, now)
	}
	return nil
}

// RefreshFuturesLatest is RefreshLatest's counterpart for the tracked NIFTY
// future — see BackfillFutures for why this is a separate entry point.
func (s *Service) RefreshFuturesLatest(ctx context.Context) error {
	futures, err := s.repo.TrackedFutures(ctx)
	if err != nil {
		return err
	}
	now := time.Now().In(market.IST)
	for _, f := range futures {
		s.refreshOne(ctx, f, now)
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
	go cronx.Safe(s.log, func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := s.BackfillFutures(c); err != nil {
			s.log.Error().Err(err).Msg("optionsalgo: initial futures backfill failed")
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
					if err := s.RefreshFuturesLatest(c); err != nil {
						s.log.Error().Err(err).Msg("optionsalgo: futures refresh failed")
					}
					// Archive the chain BEFORE the per-user loop and
					// unconditionally — deliberately not gated on anyone
					// having auto-trading enabled. Keying data collection
					// off the trading toggle would silently stop capturing
					// history the moment it's switched off, which is
					// exactly when it's likely to stay off for weeks; the
					// bid/ask/OI/Greeks lost in that window are
					// unrecoverable at any price.
					s.ArchiveChainSnapshot(c)

					// Every account with algo_enabled=true (the frontend
					// on/off toggle — see paper.Service.SetAlgoEnabled) gets
					// its own independent evaluate-and-maybe-enter pass this
					// tick. Replaces the old single-account
					// OPTIONS_ALGO_USER_EMAIL mechanism: opted-in accounts
					// are discovered fresh from the DB every tick, so a
					// toggle flipped from the frontend takes effect on the
					// very next tick, no restart needed.
					userIDs, err := s.paper.AlgoEnabledUserIDs(c)
					if err != nil {
						s.log.Error().Err(err).Msg("optionsalgo: listing algo-enabled accounts failed")
						return
					}
					for _, uid := range userIDs {
						// Manage existing positions before considering a new
						// entry — an exit this same tick frees up the
						// max-1-open-position slot for EvaluateAndMaybeEnter
						// to use right away, instead of waiting a full extra
						// minute. One account's failure doesn't stop the
						// others from being evaluated this tick.
						if _, err := s.ManageOpenPositions(c, uid); err != nil {
							s.log.Error().Err(err).Str("user_id", uid).Msg("optionsalgo: manage positions failed")
						}
						if _, err := s.EvaluateAndMaybeEnter(c, uid); err != nil {
							s.log.Error().Err(err).Str("user_id", uid).Msg("optionsalgo: evaluate entry failed")
						}
					}
				})
			}
		}
	}()
}
