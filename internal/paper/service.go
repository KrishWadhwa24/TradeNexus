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
	log     zerolog.Logger
}

// New builds the service.
func New(pool *pgxpool.Pool, ang *angel.Client, c *candles.Repo, inst *instruments.Repo,
	sig *signals.Repo, cal *calendar.Service, log zerolog.Logger) *Service {
	return &Service{pool: pool, angel: ang, candles: c, inst: inst, signals: sig, cal: cal, log: log}
}

// Account is a user's paper account.
type Account struct {
	UserID          string  `json:"user_id"`
	StartingCapital float64 `json:"starting_capital"`
	CashBalance     float64 `json:"cash_balance"`
}

// Trade is one paper trade.
type Trade struct {
	ID           int64      `json:"id"`
	userID       string     // internal owner id (not serialized)
	InstrumentID int64      `json:"instrument_id"`
	Symbol       string     `json:"symbol"`
	SignalID     *int64     `json:"signal_id,omitempty"`
	Side         string     `json:"side"`
	Quantity     int        `json:"quantity"`
	EntryPrice   float64    `json:"entry_price"`
	EntryTime    *time.Time `json:"entry_time,omitempty"`
	ExitPrice    *float64   `json:"exit_price,omitempty"`
	ExitTime     *time.Time `json:"exit_time,omitempty"`
	Status       string     `json:"status"`
	PnL          float64    `json:"pnl"`
	CurrentPrice float64    `json:"current_price,omitempty"`
	Unrealized   float64    `json:"unrealized_pnl,omitempty"`
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
		`SELECT starting_capital, cash_balance FROM paper_accounts WHERE user_id=$1::uuid`, userID).
		Scan(&a.StartingCapital, &a.CashBalance)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, nil // zeroed account
	}
	return a, err
}

// price resolves the current price: live LTP if available, else last daily close.
func (s *Service) price(ctx context.Context, inst instruments.Instrument) (float64, error) {
	if ltp, err := s.angel.GetLTP(ctx, inst.Exchange, inst.TradingSymbol, inst.SymbolToken); err == nil && ltp > 0 {
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

// Buy opens (or schedules) a paper position on the stock that produced a signal.
// Trades are only allowed on stocks with a valid BUY signal.
func (s *Service) Buy(ctx context.Context, userID string, signalID int64, qty int, source string) (Trade, error) {
	if qty <= 0 {
		return Trade{}, errors.New("quantity must be > 0")
	}
	sig, err := s.signals.GetByID(ctx, signalID)
	if err != nil {
		return Trade{}, fmt.Errorf("signal: %w", err)
	}
	if sig.Direction != "BUY" {
		return Trade{}, errors.New("paper trades are only allowed on BUY signals")
	}
	inst, err := s.inst.GetByID(ctx, sig.InstrumentID)
	if err != nil {
		return Trade{}, err
	}
	acct, err := s.GetAccount(ctx, userID)
	if err != nil {
		return Trade{}, err
	}

	open := s.cal.Cal().IsMarketOpen(time.Now().In(market.IST))
	if source == "" {
		source = "web"
	}

	if !open {
		// Schedule for next session; priced/charged at fill time.
		var id int64
		err = s.pool.QueryRow(ctx, `
			INSERT INTO paper_trades (user_id, instrument_id, signal_id, side, quantity, status, source)
			VALUES ($1::uuid, $2, $3, 'BUY', $4, 'SCHEDULED', $5) RETURNING id`,
			userID, inst.ID, signalID, qty, source).Scan(&id)
		if err != nil {
			return Trade{}, err
		}
		return s.getTrade(ctx, id)
	}

	// Execute now.
	px, err := s.price(ctx, inst)
	if err != nil {
		return Trade{}, err
	}
	cost := px * float64(qty)
	if cost > acct.CashBalance {
		return Trade{}, fmt.Errorf("insufficient cash: need %.2f, have %.2f", cost, acct.CashBalance)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Trade{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var id int64
	if err = tx.QueryRow(ctx, `
		INSERT INTO paper_trades (user_id, instrument_id, signal_id, side, quantity, entry_price, entry_time, status, source)
		VALUES ($1::uuid, $2, $3, 'BUY', $4, $5, now(), 'OPEN', $6) RETURNING id`,
		userID, inst.ID, signalID, qty, px, source).Scan(&id); err != nil {
		return Trade{}, err
	}
	if _, err = tx.Exec(ctx,
		`UPDATE paper_accounts SET cash_balance = cash_balance - $2, updated_at=now() WHERE user_id=$1::uuid`,
		userID, cost); err != nil {
		return Trade{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Trade{}, err
	}
	return s.getTrade(ctx, id)
}

// Close exits an open position at the current price and books the P&L.
func (s *Service) Close(ctx context.Context, tradeID int64) (Trade, error) {
	t, err := s.getTrade(ctx, tradeID)
	if err != nil {
		return Trade{}, err
	}
	if t.Status != "OPEN" {
		return Trade{}, fmt.Errorf("trade is %s, not OPEN", t.Status)
	}
	inst, err := s.inst.GetByID(ctx, t.InstrumentID)
	if err != nil {
		return Trade{}, err
	}
	px, err := s.price(ctx, inst)
	if err != nil {
		return Trade{}, err
	}
	pnl := (px - t.EntryPrice) * float64(t.Quantity)
	proceeds := px * float64(t.Quantity)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Trade{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err = tx.Exec(ctx, `
		UPDATE paper_trades SET status='CLOSED', exit_price=$2, exit_time=now(), pnl=$3 WHERE id=$1`,
		tradeID, px, pnl); err != nil {
		return Trade{}, err
	}
	if _, err = tx.Exec(ctx,
		`UPDATE paper_accounts SET cash_balance = cash_balance + $2, updated_at=now() WHERE user_id=$1::uuid`,
		t.userID, proceeds); err != nil {
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
		cost := px * float64(t.Quantity)
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			s.log.Error().Err(err).Int64("trade_id", id).Msg("fill scheduled: begin tx failed")
			continue
		}
		_, e1 := tx.Exec(ctx, `UPDATE paper_trades SET status='OPEN', entry_price=$2, entry_time=now() WHERE id=$1`, id, px)
		_, e2 := tx.Exec(ctx, `UPDATE paper_accounts SET cash_balance=cash_balance-$2, updated_at=now() WHERE user_id=$1::uuid`, t.userID, cost)
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

// Trades returns a user's trade history (newest first) with live valuation for
// open positions.
func (s *Service) Trades(ctx context.Context, userID string) ([]Trade, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT t.id, t.instrument_id, i.trading_symbol, t.signal_id, t.side, t.quantity,
		       t.entry_price, t.entry_time, t.exit_price, t.exit_time, t.status, t.pnl
		FROM paper_trades t JOIN instruments i ON i.id = t.instrument_id
		WHERE t.user_id = $1::uuid ORDER BY t.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Trade
	for rows.Next() {
		var t Trade
		if err := rows.Scan(&t.ID, &t.InstrumentID, &t.Symbol, &t.SignalID, &t.Side, &t.Quantity,
			&t.EntryPrice, &t.EntryTime, &t.ExitPrice, &t.ExitTime, &t.Status, &t.PnL); err != nil {
			return nil, err
		}
		if t.Status == "OPEN" {
			if inst, err := s.inst.GetByID(ctx, t.InstrumentID); err == nil {
				if px, err := s.price(ctx, inst); err == nil {
					t.CurrentPrice = px
					t.Unrealized = (px - t.EntryPrice) * float64(t.Quantity)
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

// Summary computes the P&L rollup for the profile + paper analytics views.
func (s *Service) Summary(ctx context.Context, userID string) (PnLSummary, error) {
	acct, err := s.GetAccount(ctx, userID)
	if err != nil {
		return PnLSummary{}, err
	}
	trades, err := s.Trades(ctx, userID)
	if err != nil {
		return PnLSummary{}, err
	}
	sum := PnLSummary{StartingCapital: acct.StartingCapital, CashBalance: acct.CashBalance}
	for _, t := range trades {
		switch t.Status {
		case "OPEN":
			sum.OpenPositions++
			sum.Invested += t.EntryPrice * float64(t.Quantity)
			sum.MarketValue += t.CurrentPrice * float64(t.Quantity)
			sum.Unrealized += t.Unrealized
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
	return sum, nil
}

// getTrade loads one trade (with its owner user id, used internally).
func (s *Service) getTrade(ctx context.Context, id int64) (Trade, error) {
	var t Trade
	err := s.pool.QueryRow(ctx, `
		SELECT t.id, t.user_id::text, t.instrument_id, i.trading_symbol, t.signal_id, t.side,
		       t.quantity, t.entry_price, t.entry_time, t.exit_price, t.exit_time, t.status, t.pnl
		FROM paper_trades t JOIN instruments i ON i.id = t.instrument_id
		WHERE t.id = $1`, id).
		Scan(&t.ID, &t.userID, &t.InstrumentID, &t.Symbol, &t.SignalID, &t.Side,
			&t.Quantity, &t.EntryPrice, &t.EntryTime, &t.ExitPrice, &t.ExitTime, &t.Status, &t.PnL)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, errors.New("trade not found")
	}
	return t, err
}
