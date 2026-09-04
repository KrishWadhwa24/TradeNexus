// Package paper implements the paper-trading engine: virtual capital, market-
// aware buys (execute now if open, else schedule for next session), closes,
// portfolio valuation, and P&L.
package paper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"tradenexus/internal/angel"
	"tradenexus/internal/calendar"
	"tradenexus/internal/candles"
	"tradenexus/internal/instruments"
	"tradenexus/internal/live"
	"tradenexus/internal/market"
	"tradenexus/internal/signals"
)

// Service is the paper-trading engine.
type Service struct {
	pool    *pgxpool.Pool
	angel   *angel.Client
	candles *candles.Repo
	inst    *instruments.Repo
	signals *signals.Repo
	cal     *calendar.Service
	live    *live.Hub // optional; nil falls straight back to the Angel REST path in price()
	log     zerolog.Logger
}

// New builds the service. liveHub may be nil (falls back to the slower Angel
// REST path in price() for every call instead of the live-tick cache).
func New(pool *pgxpool.Pool, ang *angel.Client, c *candles.Repo, inst *instruments.Repo,
	sig *signals.Repo, cal *calendar.Service, liveHub *live.Hub, log zerolog.Logger) *Service {
	return &Service{pool: pool, angel: ang, candles: c, inst: inst, signals: sig, cal: cal, live: liveHub, log: log}
}

// Account is a user's paper account. AlgoCashBalance is a second, separate
// balance for options-algo trades (source == SourceOptionsAlgo) — kept apart
// from CashBalance so the algo's capital and P&L never mix with the user's
// regular manual paper trading, per the user's explicit request to keep them
// separate while still sharing one account/login.
type Account struct {
	UserID          string  `json:"user_id"`
	StartingCapital float64 `json:"starting_capital"`
	CashBalance     float64 `json:"cash_balance"`
	AlgoCashBalance float64 `json:"algo_cash_balance"`
}

// availableBalance returns the balance a trade with this source draws
// against — AlgoCashBalance for options-algo trades, CashBalance for
// everything else. Centralizes the same source->balance decision the
// debit/creditBalance helpers (intraday.go) use for the actual SQL, so the
// insufficient-funds checks in OpenPosition/ConvertToDelivery can never
// check a different balance than the one that actually gets debited.
func (a Account) availableBalance(source string) float64 {
	if source == SourceOptionsAlgo {
		return a.AlgoCashBalance
	}
	return a.CashBalance
}

// Trade is one paper trade.
type Trade struct {
	ID             int64   `json:"id"`
	userID         string  // internal owner id (not serialized)
	reservedMargin float64 // internal; cash reserved for a SCHEDULED order, refunded on cancel or trued-up at fill (not serialized)
	InstrumentID   int64   `json:"instrument_id"`
	Symbol         string  `json:"symbol"`
	// OptionType is "" for equities, "CE"/"PE" for an options contract —
	// carried on Trade (not just looked up from Instrument on demand) so
	// every function that already has a Trade in hand — settleAmounts,
	// ConvertToDelivery, FillScheduled, Summary — can price it correctly
	// without a second instrument lookup. See marginFraction: an option
	// always margins at 100% of premium, never the equity leverage fraction.
	OptionType string `json:"option_type,omitempty"`
	// Source is what originated this trade ("web" for manual buys,
	// "options-algo" for the automated strategy) — carried on Trade for the
	// same reason OptionType is: every function that already has a Trade in
	// hand (closeAtPrice, closePartialAtPrice, ConvertToDelivery,
	// FillScheduled, CancelPending) needs it to know which account balance
	// (CashBalance vs AlgoCashBalance) to debit/credit, without a second
	// lookup.
	Source      string     `json:"source,omitempty"`
	SignalID    *int64     `json:"signal_id,omitempty"`
	Side        string     `json:"side"`
	ProductType string     `json:"product_type"` // DELIVERY | INTRADAY
	Quantity    int        `json:"quantity"`
	EntryPrice  float64    `json:"entry_price"`
	EntryTime   *time.Time `json:"entry_time,omitempty"`
	ExitPrice   *float64   `json:"exit_price,omitempty"`
	ExitTime    *time.Time `json:"exit_time,omitempty"`
	Status      string     `json:"status"`
	PnL         float64    `json:"pnl"`
	// CurrentPrice/Unrealized are pointers, not plain float64 — omitempty on
	// a plain float64 drops the field when it's exactly 0, which is a
	// completely legitimate value here (a position bought seconds ago,
	// price hasn't moved yet) and not the same thing as "not computed" (not
	// OPEN, or the price lookup failed). A pointer lets omitempty tell
	// those two cases apart: nil is genuinely omitted, 0.0 is sent as 0.
	CurrentPrice *float64 `json:"current_price,omitempty"`
	Unrealized   *float64 `json:"unrealized_pnl,omitempty"`
	// PendingCloseQty is set on an OPEN DELIVERY position when the user
	// closed it while the market was shut — it schedules the close instead
	// of executing at a stale price, mirroring how a DELIVERY buy schedules
	// for next open. Filled by FillScheduledCloses; nil means nothing pending.
	PendingCloseQty *int `json:"pending_close_qty,omitempty"`
}

// PnLSummary is the profit/loss rollup for the profile + paper analytics views.
type PnLSummary struct {
	StartingCapital float64 `json:"starting_capital"`
	CashBalance     float64 `json:"cash_balance"`
	Invested        float64 `json:"invested"`       // cost basis of open positions
	MarketValue     float64 `json:"market_value"`   // current value of open positions
	Unrealized      float64 `json:"unrealized_pnl"` // open positions
	RealizedTotal   float64 `json:"realized_pnl"`   // closed trades net
	BookedProfit    float64 `json:"booked_profit"`  // sum of positive closed pnl
	BookedLoss      float64 `json:"booked_loss"`    // sum of negative closed pnl
	TotalPnL        float64 `json:"total_pnl"`      // realized + unrealized
	Equity          float64 `json:"equity"`         // cash + market value
	OpenPositions   int     `json:"open_positions"`
}

// SetCapital sets the account's capital and resets cash to capital minus the
// cost of any currently-open positions.
func (s *Service) SetCapital(ctx context.Context, userID string, capital float64) (Account, error) {
	if capital < 0 {
		return Account{}, errors.New("capital must be >= 0")
	}
	var openCost float64
	_ = s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(entry_price*quantity),0) FROM paper_trades WHERE user_id=$1::uuid AND status='OPEN'`,
		userID).Scan(&openCost)

	cash := capital - openCost
	_, err := s.pool.Exec(ctx, `
		INSERT INTO paper_accounts (user_id, starting_capital, cash_balance, updated_at)
		VALUES ($1::uuid, $2, $3, now())
		ON CONFLICT (user_id) DO UPDATE
		SET starting_capital = EXCLUDED.starting_capital,
		    cash_balance     = EXCLUDED.cash_balance,
		    updated_at       = now()`, userID, capital, cash)
	if err != nil {
		return Account{}, err
	}
	return s.GetAccount(ctx, userID)
}

// GetAccount returns the account, creating a zero account if none exists.
func (s *Service) GetAccount(ctx context.Context, userID string) (Account, error) {
	var a Account
	a.UserID = userID
	err := s.pool.QueryRow(ctx,
		`SELECT starting_capital, cash_balance, algo_cash_balance FROM paper_accounts WHERE user_id=$1::uuid`, userID).
		Scan(&a.StartingCapital, &a.CashBalance, &a.AlgoCashBalance)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, nil // zeroed account
	}
	return a, err
}

// MarketOpen reports whether NSE cash is open at t — exposed so callers
// outside this package (the scheduler's fill-retry ticker) can gate calls
// to FillScheduled/FillScheduledCloses without duplicating calendar wiring.
func (s *Service) MarketOpen(t time.Time) bool {
	return s.cal.Cal().IsMarketOpen(t)
}

// displayPrice resolves a price for read-only display (Trades/Summary — the
// unrealized-P&L column, not an order fill) the same way handleDashboard's
// instrumentParams already does for Home/Analytics: live-tick cache first
// (an in-memory read, already kept warm by the user's own open live-prices
// WebSocket connection — see Server.liveInstruments/OpenInstrumentIDs), else
// the last stored daily close — never a synchronous Angel network call.
// That match matters, not just the cache-first part: price()'s fallback
// tries a live Angel LTP REST call before giving up on the candle close, so
// using price() here would still leave a slow path (network-bound, queued
// behind the same rate limiter the intraday-cache job hammers) for any
// position whose live tick hasn't arrived yet — precisely the failure mode
// this function exists to remove. The true current price still shows up
// moments later regardless, once its tick arrives over the already-open
// WebSocket (see Paper.jsx's connectLivePrices merge) — nothing here is
// ever worth blocking a page load on. Never used for an actual fill —
// pricing a real order still goes through price() unchanged, so execution
// behavior is untouched by this.
//
// Trades() used to call price() directly for every OPEN position, in a
// per-row loop — each one a blocking Angel REST call queued behind that same
// rate limiter, which is exactly why /paper/trades and /paper/summary were
// measured taking 10-30s (some hitting the router's 30s timeout and
// returning a truncated response) instead of near-instant.
//
// A cached tick is only trusted if it's from today (IST) — a tick from a
// prior session (the instrument hasn't traded yet today, or its WebSocket
// subscription only picked it up after a stale value was already cached) is
// not "the latest price."
func (s *Service) displayPrice(ctx context.Context, inst instruments.Instrument) (float64, error) {
	if s.live != nil {
		if tick, ok := s.live.GetLastTick(inst.Exchange, inst.SymbolToken); ok && tick.Price > 0 && market.SameISTDate(tick.Timestamp, time.Now()) {
			return tick.Price, nil
		}
	}
	daily, err := s.candles.GetDaily(ctx, inst.ID)
	if err != nil {
		return 0, err
	}
	if len(daily) == 0 {
		return 0, errors.New("no price available (no candles); sync the instrument first")
	}
	return daily[len(daily)-1].Close, nil
}

// price resolves the current price: live LTP if available, else last daily
// close. For an option specifically, uses the bid-ask-aware Quote-FULL path
// instead of plain GetLTP — confirmed live that a thinly-traded option's LTP
// can be a stale print sitting entirely outside its own live bid/ask (see
// angel.QuoteFull.EffectivePrice), which GetLTP alone has no way to detect
// (it returns only LTP, no depth). Equity behavior is completely unchanged —
// this only adds a new branch in front of it for OptionType != "".
func (s *Service) price(ctx context.Context, inst instruments.Instrument) (float64, error) {
	if inst.OptionType != "" {
		if quotes, err := s.angel.GetOptionQuoteFull(ctx, inst.Exchange, []string{inst.SymbolToken}); err == nil && len(quotes) > 0 && quotes[0].LTP > 0 {
			return quotes[0].EffectivePrice(), nil
		}
	} else if ltp, err := s.angel.GetLTP(ctx, inst.Exchange, inst.TradingSymbol, inst.SymbolToken); err == nil && ltp > 0 {
		return ltp, nil
	}
	daily, err := s.candles.GetDaily(ctx, inst.ID)
	if err != nil {
		return 0, err
	}
	if len(daily) == 0 {
		return 0, errors.New("no price available (no candles); sync the instrument first")
	}
	return daily[len(daily)-1].Close, nil
}

// Buy opens (or schedules) a paper position on the stock that produced a
// signal. Trades are only allowed on stocks with a valid BUY signal. Kept
// as a thin wrapper over OpenPosition (see intraday.go) for Scanner.jsx's
// existing flow — always a DELIVERY long, exactly today's behavior.
func (s *Service) Buy(ctx context.Context, userID string, signalID int64, qty int, source string) (Trade, error) {
	sig, err := s.signals.GetByID(ctx, signalID)
	if err != nil {
		return Trade{}, fmt.Errorf("signal: %w", err)
	}
	if sig.Direction != "BUY" {
		return Trade{}, errors.New("paper trades are only allowed on BUY signals")
	}
	return s.OpenPosition(ctx, userID, sig.InstrumentID, qty, SideBuy, ProductDelivery, &signalID, source)
}

// ClosePartial exits qty shares of an open position, crediting the
// proportional margin+P&L to cash. qty<=0 or >= the position's full
// quantity closes the entire position (today's original full-close
// behavior). A smaller qty splits off a new CLOSED row for the exited
// shares via closePartialAtPrice and leaves the remainder OPEN at its
// original average entry price, so the user can sell down a position
// instead of being forced to exit all of it at once.
//
// Market-hours rule mirrors OpenPosition exactly: an INTRADAY close can
// only execute live (9:15am-3:20pm IST) — no scheduling, same as an
// intraday buy — since an unconverted intraday position never survives
// past the cutoff anyway (SquareOffIntraday). A DELIVERY close, when the
// market's shut, schedules instead of executing at a stale price — same
// AMO-style deferral as a DELIVERY buy — rather than either rejecting it or
// silently pricing it off the last close.
func (s *Service) ClosePartial(ctx context.Context, tradeID int64, qty int) (Trade, error) {
	t, err := s.getTrade(ctx, tradeID)
	if err != nil {
		return Trade{}, err
	}
	if t.Status != "OPEN" {
		return Trade{}, fmt.Errorf("trade is %s, not OPEN", t.Status)
	}
	if qty <= 0 || qty > t.Quantity {
		qty = t.Quantity
	}

	if t.ProductType == ProductIntraday {
		if !intradayWindowOpen(s.cal, time.Now()) {
			return Trade{}, errors.New("intraday positions can only be closed during live market hours (9:15am-3:20pm IST) — held past the cutoff, they're auto squared-off")
		}
	} else if !s.cal.Cal().IsMarketOpen(time.Now().In(market.IST)) {
		if _, err := s.pool.Exec(ctx, `UPDATE paper_trades SET pending_close_qty=$2 WHERE id=$1`, tradeID, qty); err != nil {
			return Trade{}, err
		}
		return s.getTrade(ctx, tradeID)
	}

	inst, err := s.inst.GetByID(ctx, t.InstrumentID)
	if err != nil {
		return Trade{}, err
	}
	px, err := s.price(ctx, inst)
	if err != nil {
		return Trade{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Trade{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if qty == t.Quantity {
		if _, err := closeAtPrice(ctx, tx, t, px); err != nil {
			return Trade{}, err
		}
	} else if err := closePartialAtPrice(ctx, tx, t, qty, px); err != nil {
		return Trade{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE paper_trades SET pending_close_qty=NULL WHERE id=$1`, tradeID); err != nil {
		return Trade{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Trade{}, err
	}
	return s.getTrade(ctx, tradeID)
}

// FillScheduledIfMarketOpen fills SCHEDULED trades only if the market is open
// right now. Run once on server startup: if the server (or the market) was
// closed when a trade was scheduled and is already open by the time the
// process comes up, this converts it immediately instead of leaving it
// stranded until the next FillScheduledCron tick.
func (s *Service) FillScheduledIfMarketOpen(ctx context.Context) (int, error) {
	if !s.cal.Cal().IsMarketOpen(time.Now().In(market.IST)) {
		return 0, nil
	}
	return s.FillScheduled(ctx)
}

// FillScheduled executes SCHEDULED trades — run at market open by the scheduler.
func (s *Service) FillScheduled(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id FROM paper_trades WHERE status='SCHEDULED' ORDER BY created_at`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()

	filled := 0
	for _, id := range ids {
		t, err := s.getTrade(ctx, id)
		if err != nil {
			s.log.Error().Err(err).Int64("trade_id", id).Msg("fill scheduled: load trade failed")
			continue
		}
		inst, err := s.inst.GetByID(ctx, t.InstrumentID)
		if err != nil {
			s.log.Error().Err(err).Int64("trade_id", id).Msg("fill scheduled: load instrument failed")
			continue
		}
		px, err := s.price(ctx, inst)
		if err != nil {
			s.log.Error().Err(err).Int64("trade_id", id).Str("symbol", inst.TradingSymbol).Msg("fill scheduled: price lookup failed")
			continue
		}
		// Reserved margin was already debited at schedule time (at
		// tonight's price); only the difference against the actual fill
		// price needs to move now, positive or negative.
		cost := marginFraction(t.ProductType, t.OptionType != "") * px * float64(t.Quantity)
		delta := cost - t.reservedMargin
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			s.log.Error().Err(err).Int64("trade_id", id).Msg("fill scheduled: begin tx failed")
			continue
		}
		_, e1 := tx.Exec(ctx, `UPDATE paper_trades SET status='OPEN', entry_price=$2, entry_time=now(), reserved_margin=0 WHERE id=$1`, id, px)
		e2 := debitBalance(ctx, tx, t.userID, t.Source, delta)
		if e1 != nil || e2 != nil {
			_ = tx.Rollback(ctx)
			s.log.Error().Err(errors.Join(e1, e2)).Int64("trade_id", id).Msg("fill scheduled: update failed")
			continue
		}
		if err := tx.Commit(ctx); err != nil {
			s.log.Error().Err(err).Int64("trade_id", id).Msg("fill scheduled: commit failed")
			continue
		}
		filled++
	}
	return filled, nil
}

// FillScheduledClosesIfMarketOpen mirrors FillScheduledIfMarketOpen for the
// close side — boot-time catch-up so a DELIVERY close scheduled while the
// server (or the market) was down doesn't sit stranded until the next
// FillScheduledCron tick.
func (s *Service) FillScheduledClosesIfMarketOpen(ctx context.Context) (int, error) {
	if !s.cal.Cal().IsMarketOpen(time.Now().In(market.IST)) {
		return 0, nil
	}
	return s.FillScheduledCloses(ctx)
}

// FillScheduledCloses executes every OPEN position with a pending close at
// the current price — run at market open by the scheduler, alongside
// FillScheduled. Reuses closeAtPrice/closePartialAtPrice, the same
// settlement math a live close uses, so a scheduled close can never compute
// a different number than an immediate one would have.
func (s *Service) FillScheduledCloses(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id FROM paper_trades WHERE status='OPEN' AND pending_close_qty IS NOT NULL ORDER BY created_at`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()

	filled := 0
	for _, id := range ids {
		t, err := s.getTrade(ctx, id)
		if err != nil {
			s.log.Error().Err(err).Int64("trade_id", id).Msg("fill scheduled close: load trade failed")
			continue
		}
		if t.PendingCloseQty == nil {
			continue // raced with a manual close/cancel between the SELECT and here
		}
		qty := *t.PendingCloseQty
		if qty <= 0 || qty > t.Quantity {
			qty = t.Quantity
		}
		inst, err := s.inst.GetByID(ctx, t.InstrumentID)
		if err != nil {
			s.log.Error().Err(err).Int64("trade_id", id).Msg("fill scheduled close: load instrument failed")
			continue
		}
		px, err := s.price(ctx, inst)
		if err != nil {
			s.log.Error().Err(err).Int64("trade_id", id).Str("symbol", inst.TradingSymbol).Msg("fill scheduled close: price lookup failed")
			continue
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			s.log.Error().Err(err).Int64("trade_id", id).Msg("fill scheduled close: begin tx failed")
			continue
		}
		var closeErr error
		if qty == t.Quantity {
			_, closeErr = closeAtPrice(ctx, tx, t, px)
		} else {
			closeErr = closePartialAtPrice(ctx, tx, t, qty, px)
		}
		if closeErr != nil {
			_ = tx.Rollback(ctx)
			s.log.Error().Err(closeErr).Int64("trade_id", id).Msg("fill scheduled close: settle failed")
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE paper_trades SET pending_close_qty=NULL WHERE id=$1`, id); err != nil {
			_ = tx.Rollback(ctx)
			s.log.Error().Err(err).Int64("trade_id", id).Msg("fill scheduled close: clear pending failed")
			continue
		}
		if err := tx.Commit(ctx); err != nil {
			s.log.Error().Err(err).Int64("trade_id", id).Msg("fill scheduled close: commit failed")
			continue
		}
		filled++
	}
	return filled, nil
}

// CancelPending cancels a not-yet-executed order the user placed by mistake
// or changed their mind on: either a SCHEDULED buy that hasn't filled yet,
// or an OPEN position's pending close that hasn't executed yet. A pending
// close never reserved anything (the shares are already paid for), so
// that's just clearing the pending state. A SCHEDULED buy did reserve
// margin at schedule time (see OpenPosition) — refund it in full.
func (s *Service) CancelPending(ctx context.Context, tradeID int64) (Trade, error) {
	t, err := s.getTrade(ctx, tradeID)
	if err != nil {
		return Trade{}, err
	}
	switch {
	case t.Status == "SCHEDULED":
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return Trade{}, err
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		if _, err := tx.Exec(ctx, `UPDATE paper_trades SET status='CANCELLED', reserved_margin=0 WHERE id=$1`, tradeID); err != nil {
			return Trade{}, err
		}
		if err := creditBalance(ctx, tx, t.userID, t.Source, t.reservedMargin); err != nil {
			return Trade{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Trade{}, err
		}
	case t.Status == "OPEN" && t.PendingCloseQty != nil:
		if _, err := s.pool.Exec(ctx, `UPDATE paper_trades SET pending_close_qty=NULL WHERE id=$1`, tradeID); err != nil {
			return Trade{}, err
		}
	default:
		return Trade{}, fmt.Errorf("trade has nothing pending to cancel")
	}
	return s.getTrade(ctx, tradeID)
}

// Trades returns a user's trade history (newest first) with live valuation for
// open positions.
func (s *Service) Trades(ctx context.Context, userID string) ([]Trade, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.instrument_id, i.trading_symbol, COALESCE(i.option_type, ''), COALESCE(t.source, ''), t.signal_id, t.side, t.product_type, t.quantity,
		       t.entry_price, t.entry_time, t.exit_price, t.exit_time, t.status, t.pnl, t.pending_close_qty
		FROM paper_trades t JOIN instruments i ON i.id = t.instrument_id
		WHERE t.user_id = $1::uuid ORDER BY t.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Trade
	for rows.Next() {
		var t Trade
		if err := rows.Scan(&t.ID, &t.InstrumentID, &t.Symbol, &t.OptionType, &t.Source, &t.SignalID, &t.Side, &t.ProductType, &t.Quantity,
			&t.EntryPrice, &t.EntryTime, &t.ExitPrice, &t.ExitTime, &t.Status, &t.PnL, &t.PendingCloseQty); err != nil {
			return nil, err
		}
		if t.Status == "OPEN" {
			if inst, err := s.inst.GetByID(ctx, t.InstrumentID); err == nil {
				if px, err := s.displayPrice(ctx, inst); err == nil {
					u := unrealizedPnL(t.Side, t.EntryPrice, px, t.Quantity)
					t.CurrentPrice = &px
					t.Unrealized = &u
				}
			}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// OpenInstrumentIDs returns the distinct instruments backing this user's
// currently OPEN paper trades — used to add paper positions to the live-
// price WebSocket subscription (see Server.liveInstruments) without paying
// the per-trade Angel LTP REST-call cost that Trades incurs above.
func (s *Service) OpenInstrumentIDs(ctx context.Context, userID string) ([]int64, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT instrument_id FROM paper_trades WHERE user_id = $1::uuid AND status = 'OPEN'`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// aggregateSummary is the shared P&L rollup math behind both Summary and
// AlgoSummary — same aggregation, just over a different (pre-filtered)
// trade slice and cash balance, so the two summaries can never compute this
// differently.
func aggregateSummary(startingCapital, cashBalance float64, trades []Trade) PnLSummary {
	sum := PnLSummary{StartingCapital: startingCapital, CashBalance: cashBalance}
	for _, t := range trades {
		switch t.Status {
		case "OPEN":
			sum.OpenPositions++
			// Invested = what's actually locked in cash_balance for this
			// position (full notional for DELIVERY, 20% margin for
			// INTRADAY) — NOT full notional for a leveraged position,
			// since that cash was never debited. MarketValue = what would
			// come back to cash_balance if closed right now (Invested +
			// Unrealized) — this generalizes the pre-margin formula
			// exactly: for a DELIVERY long it reduces to today's original
			// EntryPrice*qty / CurrentPrice*qty.
			invested := marginFraction(t.ProductType, t.OptionType != "") * t.EntryPrice * float64(t.Quantity)
			sum.Invested += invested
			var unrealized float64
			if t.Unrealized != nil {
				unrealized = *t.Unrealized
			}
			sum.MarketValue += invested + unrealized
			sum.Unrealized += unrealized
		case "CLOSED":
			sum.RealizedTotal += t.PnL
			if t.PnL >= 0 {
				sum.BookedProfit += t.PnL
			} else {
				sum.BookedLoss += t.PnL
			}
		}
	}
	sum.TotalPnL = sum.RealizedTotal + sum.Unrealized
	sum.Equity = sum.CashBalance + sum.MarketValue
	return sum
}

// Summary computes the P&L rollup for the profile + paper analytics views —
// manual/equity trading only. Explicitly excludes options-algo trades (which
// share the same user_id but a separate balance, AlgoCashBalance) so the
// regular dashboard is never polluted by algo P&L; see AlgoSummary for that.
func (s *Service) Summary(ctx context.Context, userID string) (PnLSummary, error) {
	acct, err := s.GetAccount(ctx, userID)
	if err != nil {
		return PnLSummary{}, err
	}
	trades, err := s.Trades(ctx, userID)
	if err != nil {
		return PnLSummary{}, err
	}
	nonAlgo := make([]Trade, 0, len(trades))
	for _, t := range trades {
		if t.Source != SourceOptionsAlgo {
			nonAlgo = append(nonAlgo, t)
		}
	}
	return aggregateSummary(acct.StartingCapital, acct.CashBalance, nonAlgo), nil
}

// AlgoSummary is Summary's counterpart for options-algo trades — same
// aggregation, filtered to only source==SourceOptionsAlgo trades, against
// AlgoCashBalance instead of CashBalance. Powers the algo-trading UI section.
func (s *Service) AlgoSummary(ctx context.Context, userID string) (PnLSummary, error) {
	acct, err := s.GetAccount(ctx, userID)
	if err != nil {
		return PnLSummary{}, err
	}
	trades, err := s.Trades(ctx, userID)
	if err != nil {
		return PnLSummary{}, err
	}
	algoTrades := make([]Trade, 0, len(trades))
	for _, t := range trades {
		if t.Source == SourceOptionsAlgo {
			algoTrades = append(algoTrades, t)
		}
	}
	return aggregateSummary(acct.StartingCapital, acct.AlgoCashBalance, algoTrades), nil
}

// SetAlgoCapital sets the algo account's capital and resets algo_cash_balance
// to capital minus the cost of any currently-open algo positions — mirrors
// SetCapital exactly, scoped to source=options-algo instead of all trades.
func (s *Service) SetAlgoCapital(ctx context.Context, userID string, capital float64) (Account, error) {
	if capital < 0 {
		return Account{}, errors.New("capital must be >= 0")
	}
	var openCost float64
	_ = s.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(entry_price*quantity),0) FROM paper_trades WHERE user_id=$1::uuid AND status='OPEN' AND source=$2`,
		userID, SourceOptionsAlgo).Scan(&openCost)

	algoCash := capital - openCost
	// Upsert, not a plain UPDATE: a user who's never touched paper trading
	// yet (no GetAccount/SetCapital call, so no paper_accounts row at all)
	// would otherwise have this silently affect zero rows. starting_capital/
	// cash_balance default to 0 for a brand-new row here — harmless, and
	// self-corrects the moment the user's own SetCapital runs (its ON
	// CONFLICT explicitly overwrites both).
	_, err := s.pool.Exec(ctx, `
		INSERT INTO paper_accounts (user_id, starting_capital, cash_balance, algo_cash_balance, updated_at)
		VALUES ($1::uuid, 0, 0, $2, now())
		ON CONFLICT (user_id) DO UPDATE SET algo_cash_balance = EXCLUDED.algo_cash_balance, updated_at = now()`,
		userID, algoCash)
	if err != nil {
		return Account{}, err
	}
	return s.GetAccount(ctx, userID)
}

// getTrade loads one trade (with its owner user id, used internally).
func (s *Service) getTrade(ctx context.Context, id int64) (Trade, error) {
	var t Trade
	err := s.pool.QueryRow(ctx, `
		SELECT t.id, t.user_id::text, t.instrument_id, i.trading_symbol, COALESCE(i.option_type, ''), COALESCE(t.source, ''), t.signal_id, t.side, t.product_type,
		       t.quantity, t.entry_price, t.entry_time, t.exit_price, t.exit_time, t.status, t.pnl,
		       t.pending_close_qty, t.reserved_margin
		FROM paper_trades t JOIN instruments i ON i.id = t.instrument_id
		WHERE t.id = $1`, id).
		Scan(&t.ID, &t.userID, &t.InstrumentID, &t.Symbol, &t.OptionType, &t.Source, &t.SignalID, &t.Side, &t.ProductType,
			&t.Quantity, &t.EntryPrice, &t.EntryTime, &t.ExitPrice, &t.ExitTime, &t.Status, &t.PnL,
			&t.PendingCloseQty, &t.reservedMargin)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, errors.New("trade not found")
	}
	return t, err
}
