package paper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"tradenexus/internal/calendar"
	"tradenexus/internal/instruments"
	"tradenexus/internal/market"
)

const (
	SideBuy  = "BUY"
	SideSell = "SELL"

	ProductDelivery = "DELIVERY"
	ProductIntraday = "INTRADAY"

	// intradayMarginFraction is the paper-trading margin requirement for
	// any INTRADAY position (long or short) — 20% of notional, i.e. 5x
	// leverage, matching real brokers' typical MIS margin. DELIVERY always
	// costs its full notional (fraction 1.0, no leverage).
	intradayMarginFraction = 0.2

	// intradayCutoffHour/Min is the daily auto square-off time (3:20pm
	// IST) — every OPEN intraday position still open at this time gets
	// force-closed, mirroring a real broker's MIS auto square-off. Keep
	// this in sync with config's SQUARE_OFF_INTRADAY_CRON default.
	intradayCutoffHour = 15
	intradayCutoffMin  = 20
)

// marginFraction is the fraction of notional (price*qty) charged as margin.
// isOption always forces 1.0 regardless of productType: buying an option
// costs its full premium upfront, always — there's no leverage/margin-
// financing concept for a long option the way there is for equity intraday
// trading. The equity 20%/100% intraday/delivery split below is unaffected;
// this only adds a new case in front of it.
func marginFraction(productType string, isOption bool) float64 {
	if isOption {
		return 1.0
	}
	if productType == ProductIntraday {
		return intradayMarginFraction
	}
	return 1.0
}

// validateOptionLotSize rejects an order whose quantity isn't a whole
// multiple of the contract's lot size — real options can only ever
// transact in whole lots, unlike equities where any share count is valid.
// A no-op for non-option instruments (OptionType == ""). Pulled out as a
// pure function, same convention as weightedAvgEntry/settleAmounts, so it's
// unit-testable without a DB transaction.
func validateOptionLotSize(inst instruments.Instrument, qty int) error {
	if inst.OptionType == "" {
		return nil
	}
	if inst.LotSize <= 0 || qty%inst.LotSize != 0 {
		return fmt.Errorf("quantity must be a multiple of the lot size (%d) for this contract", inst.LotSize)
	}
	return nil
}

// intradayWindowOpen reports whether new intraday/short positions may be
// opened right now — market open AND before today's auto square-off cutoff.
// Unlike DELIVERY orders, intraday orders are never scheduled for the next
// session: a position that can't be squared off same-day isn't intraday.
func intradayWindowOpen(cal *calendar.Service, t time.Time) bool {
	now := t.In(market.IST)
	if !cal.Cal().IsMarketOpen(now) {
		return false
	}
	cutoff := time.Date(now.Year(), now.Month(), now.Day(), intradayCutoffHour, intradayCutoffMin, 0, 0, market.IST)
	return now.Before(cutoff)
}

// unrealizedPnL is side-aware: a long profits when price rises, a short
// profits when price falls.
func unrealizedPnL(side string, entryPrice, currentPrice float64, qty int) float64 {
	if side == SideSell {
		return (entryPrice - currentPrice) * float64(qty)
	}
	return (currentPrice - entryPrice) * float64(qty)
}

// OpenPosition opens (or schedules) a paper position on ANY instrument —
// the generalized entry point behind both Buy (signal-gated, kept as-is
// for Scanner.jsx) and the search-any-stock Trade screen. side is "BUY"
// (long) or "SELL" (short); productType is "DELIVERY" or "INTRADAY".
// Short selling is only ever allowed intraday — a naked short can't be
// held overnight in a real cash-segment account without F&O/SLB infra,
// which is out of scope here, so this mirrors that constraint rather than
// simulating it.
func (s *Service) OpenPosition(ctx context.Context, userID string, instrumentID int64, qty int, side, productType string, signalID *int64, source string) (Trade, error) {
	if qty <= 0 {
		return Trade{}, errors.New("quantity must be > 0")
	}
	if side != SideBuy && side != SideSell {
		return Trade{}, errors.New("side must be BUY or SELL")
	}
	if productType != ProductDelivery && productType != ProductIntraday {
		return Trade{}, errors.New("product_type must be DELIVERY or INTRADAY")
	}
	if side == SideSell && productType == ProductDelivery {
		return Trade{}, errors.New("short selling is only available intraday")
	}
	if productType == ProductIntraday && !intradayWindowOpen(s.cal, time.Now()) {
		return Trade{}, errors.New("intraday and short-sell orders can only be placed during live market hours (9:15am-3:20pm IST)")
	}

	inst, err := s.inst.GetByID(ctx, instrumentID)
	if err != nil {
		return Trade{}, err
	}
	if err := validateOptionLotSize(inst, qty); err != nil {
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
		// Schedule for next session — reserve margin now, at the current
		// (last-close) price, so cash_balance immediately reflects it;
		// otherwise available cash wouldn't account for pending scheduled
		// orders and a user could schedule more than they can actually
		// afford. Refunded in full on cancel (CancelPending); trued-up
		// against the real fill price at market open (FillScheduled),
		// since the actual execution price can differ from tonight's price
		// after an overnight gap.
		schedPx, err := s.price(ctx, inst)
		if err != nil {
			return Trade{}, err
		}
		margin := marginFraction(productType, inst.OptionType != "") * schedPx * float64(qty)
		if margin > acct.CashBalance {
			return Trade{}, fmt.Errorf("insufficient cash: need %.2f margin, have %.2f", margin, acct.CashBalance)
		}

		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return Trade{}, err
		}
		defer tx.Rollback(ctx) //nolint:errcheck

		var id int64
		if err = tx.QueryRow(ctx, `
			INSERT INTO paper_trades (user_id, instrument_id, signal_id, side, product_type, quantity, reserved_margin, status, source)
			VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, 'SCHEDULED', $8) RETURNING id`,
			userID, inst.ID, signalID, side, productType, qty, margin, source).Scan(&id); err != nil {
			return Trade{}, err
		}
		if _, err = tx.Exec(ctx,
			`UPDATE paper_accounts SET cash_balance = cash_balance - $2, updated_at=now() WHERE user_id=$1::uuid`,
			userID, margin); err != nil {
			return Trade{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return Trade{}, err
		}
		return s.getTrade(ctx, id)
	}

	// Execute now.
	px, err := s.price(ctx, inst)
	if err != nil {
		return Trade{}, err
	}
	margin := marginFraction(productType, inst.OptionType != "") * px * float64(qty)
	if margin > acct.CashBalance {
		return Trade{}, fmt.Errorf("insufficient cash: need %.2f margin, have %.2f", margin, acct.CashBalance)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Trade{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	id, err := mergeOrOpen(ctx, tx, userID, inst.ID, side, productType, signalID, source, qty, px)
	if err != nil {
		return Trade{}, err
	}
	if _, err = tx.Exec(ctx,
		`UPDATE paper_accounts SET cash_balance = cash_balance - $2, updated_at=now() WHERE user_id=$1::uuid`,
		userID, margin); err != nil {
		return Trade{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Trade{}, err
	}
	return s.getTrade(ctx, id)
}

// weightedAvgEntry computes the size-weighted average entry price when a
// second fill of qty2@price2 is merged into an existing qty1@price1
// position. Pure and unit-tested (intraday_test.go) — the DB-touching
// mergeOrOpen just calls this.
func weightedAvgEntry(qty1 int, price1 float64, qty2 int, price2 float64) (int, float64) {
	totalQty := qty1 + qty2
	return totalQty, (price1*float64(qty1) + price2*float64(qty2)) / float64(totalQty)
}

// mergeOrOpen inserts a new OPEN paper_trades row, or — if the user already
// holds an OPEN position in the same instrument/side/product_type — merges
// into it: quantity sums and entry_price becomes the weighted average of
// the two fills (weightedAvgEntry). This mirrors a real broker: buying more
// of a stock you already hold doesn't create a second position, it moves
// your average cost.
//
// The caller keeps debiting margin for only this fill's qty/price — that
// stays correct after any number of merges because the margin fraction is
// linear (avgPrice*totalQty*fraction == sum of each fill's
// price*qty*fraction), the same property that lets settleAmounts settle
// from current quantity/entry_price alone with no separate ledger.
//
// ponytail: only merges already-OPEN positions, not two pending SCHEDULED
// orders for the same stock (FillScheduled doesn't route through this) —
// narrower case of buying pre-market on top of an existing multi-day
// holding; fold FillScheduled through this helper too if that gap matters.
func mergeOrOpen(ctx context.Context, tx pgx.Tx, userID string, instrumentID int64,
	side, productType string, signalID *int64, source string, qty int, price float64) (int64, error) {
	var existingID int64
	var existingQty int
	var existingPrice float64
	err := tx.QueryRow(ctx, `
		SELECT id, quantity, entry_price FROM paper_trades
		WHERE user_id=$1::uuid AND instrument_id=$2 AND side=$3 AND product_type=$4 AND status='OPEN'
		FOR UPDATE`,
		userID, instrumentID, side, productType).Scan(&existingID, &existingQty, &existingPrice)

	if errors.Is(err, pgx.ErrNoRows) {
		var id int64
		err = tx.QueryRow(ctx, `
			INSERT INTO paper_trades (user_id, instrument_id, signal_id, side, product_type, quantity, entry_price, entry_time, status, source)
			VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, now(), 'OPEN', $8) RETURNING id`,
			userID, instrumentID, signalID, side, productType, qty, price, source).Scan(&id)
		return id, err
	}
	if err != nil {
		return 0, err
	}

	newQty, newPrice := weightedAvgEntry(existingQty, existingPrice, qty, price)
	_, err = tx.Exec(ctx, `UPDATE paper_trades SET quantity=$2, entry_price=$3 WHERE id=$1`, existingID, newQty, newPrice)
	return existingID, err
}

// settleAmounts is the pure math behind closing a trade: the realized P&L
// (side-aware) and the total amount to credit back to cash_balance (the
// margin originally reserved for this trade's current product_type, plus
// that P&L). Pulled out of closeAtPrice so it's unit-testable without a DB
// transaction — see intraday_test.go.
func settleAmounts(t Trade, exitPrice float64) (pnl, settlement float64) {
	pnl = unrealizedPnL(t.Side, t.EntryPrice, exitPrice, t.Quantity)
	settlement = marginFraction(t.ProductType, t.OptionType != "")*t.EntryPrice*float64(t.Quantity) + pnl
	return pnl, settlement
}

// closeAtPrice settles a trade at the given exit price inside the given
// tx — the single source of truth for side/product-type-aware P&L and
// cash settlement, shared by a user-initiated Close and the intraday auto
// square-off job so the two paths can never compute different numbers for
// the same trade. Returns the realized P&L.
//
// Settlement = the margin originally reserved for this trade's current
// product_type, plus realized P&L. For a DELIVERY long this reduces to
// exactly today's `cash_balance += exitPrice*qty` (fraction=1.0, pnl =
// (exit-entry)*qty), so existing behavior is unchanged for that case.
func closeAtPrice(ctx context.Context, tx pgx.Tx, t Trade, exitPrice float64) (float64, error) {
	pnl, settlement := settleAmounts(t, exitPrice)

	if _, err := tx.Exec(ctx, `
		UPDATE paper_trades SET status='CLOSED', exit_price=$2, exit_time=now(), pnl=$3 WHERE id=$1`,
		t.ID, exitPrice, pnl); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE paper_accounts SET cash_balance = cash_balance + $2, updated_at=now() WHERE user_id=$1::uuid`,
		t.userID, settlement); err != nil {
		return 0, err
	}
	return pnl, nil
}

// closePartialAtPrice sells qty shares off an OPEN position without
// closing the whole thing: it books a new CLOSED row for just the exited
// qty (its own entry/exit price and realized P&L — the "lot" that shows up
// in trade history), and reduces the original row's quantity by qty,
// leaving entry_price untouched — selling part of a position doesn't
// change the average cost of what's left, mirroring a real broker.
func closePartialAtPrice(ctx context.Context, tx pgx.Tx, t Trade, qty int, exitPrice float64) error {
	lot := t
	lot.Quantity = qty
	pnl, settlement := settleAmounts(lot, exitPrice)

	if _, err := tx.Exec(ctx, `
		INSERT INTO paper_trades (user_id, instrument_id, signal_id, side, product_type, quantity, entry_price, entry_time, exit_price, exit_time, status, pnl)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, now(), 'CLOSED', $10)`,
		t.userID, t.InstrumentID, t.SignalID, t.Side, t.ProductType, qty, t.EntryPrice, t.EntryTime, exitPrice, pnl); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE paper_trades SET quantity = quantity - $2 WHERE id=$1`,
		t.ID, qty); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE paper_accounts SET cash_balance = cash_balance + $2, updated_at=now() WHERE user_id=$1::uuid`,
		t.userID, settlement); err != nil {
		return err
	}
	return nil
}

// ConvertToDelivery upgrades an OPEN intraday LONG position to a delivery
// holding by charging the remaining margin (20% is already reserved; this
// charges the other 80%). After this it behaves exactly like a normal
// delivery buy: no auto square-off, held indefinitely, closed only when
// the user chooses to. Short positions can never convert — mirrors real
// cash-segment rules (see OpenPosition) — and since SquareOffIntraday
// guarantees no unconverted intraday position survives past the daily
// cutoff, "still OPEN + INTRADAY" already implies "before cutoff," so no
// separate time check is needed here.
func (s *Service) ConvertToDelivery(ctx context.Context, tradeID int64) (Trade, error) {
	t, err := s.getTrade(ctx, tradeID)
	if err != nil {
		return Trade{}, err
	}
	if t.Status != "OPEN" {
		return Trade{}, fmt.Errorf("trade is %s, not OPEN", t.Status)
	}
	if t.ProductType != ProductIntraday {
		return Trade{}, errors.New("trade is already delivery")
	}
	if t.Side != SideBuy {
		return Trade{}, errors.New("short positions can't be converted to delivery")
	}

	remaining := (1 - marginFraction(ProductIntraday, t.OptionType != "")) * t.EntryPrice * float64(t.Quantity)
	acct, err := s.GetAccount(ctx, t.userID)
	if err != nil {
		return Trade{}, err
	}
	if remaining > acct.CashBalance {
		return Trade{}, fmt.Errorf("insufficient cash to convert: need %.2f more, have %.2f", remaining, acct.CashBalance)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Trade{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx,
		`UPDATE paper_accounts SET cash_balance = cash_balance - $2, updated_at=now() WHERE user_id=$1::uuid`,
		t.userID, remaining); err != nil {
		return Trade{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE paper_trades SET product_type='DELIVERY' WHERE id=$1`, tradeID); err != nil {
		return Trade{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Trade{}, err
	}
	return s.getTrade(ctx, tradeID)
}

// SquareOffIntraday force-closes every OPEN intraday position (long or
// short) at the current price — run daily at the intraday cutoff (see
// SquareOffIntradayCron / SquareOffIntradayIfPastCutoff), mirroring how a
// real broker auto-squares-off intraday positions you didn't close or
// convert yourself.
func (s *Service) SquareOffIntraday(ctx context.Context) (int, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id FROM paper_trades WHERE status='OPEN' AND product_type='INTRADAY' ORDER BY created_at`)
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

	closed := 0
	for _, id := range ids {
		t, err := s.getTrade(ctx, id)
		if err != nil {
			s.log.Error().Err(err).Int64("trade_id", id).Msg("square off intraday: load trade failed")
			continue
		}
		inst, err := s.inst.GetByID(ctx, t.InstrumentID)
		if err != nil {
			s.log.Error().Err(err).Int64("trade_id", id).Msg("square off intraday: load instrument failed")
			continue
		}
		px, err := s.price(ctx, inst)
		if err != nil {
			s.log.Error().Err(err).Int64("trade_id", id).Str("symbol", inst.TradingSymbol).Msg("square off intraday: price lookup failed")
			continue
		}
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			s.log.Error().Err(err).Int64("trade_id", id).Msg("square off intraday: begin tx failed")
			continue
		}
		if _, err := closeAtPrice(ctx, tx, t, px); err != nil {
			_ = tx.Rollback(ctx)
			s.log.Error().Err(err).Int64("trade_id", id).Msg("square off intraday: settle failed")
			continue
		}
		if err := tx.Commit(ctx); err != nil {
			s.log.Error().Err(err).Int64("trade_id", id).Msg("square off intraday: commit failed")
			continue
		}
		closed++
	}
	s.log.Info().Int("closed", closed).Msg("square off intraday: done")
	return closed, nil
}

// SquareOffIntradayIfPastCutoff runs SquareOffIntraday once at server
// startup if we're already past today's intraday cutoff — a cron trigger
// only fires if the process happens to be running at that exact moment,
// so if the server was down at 3:20pm and comes back up any time later
// (3:45pm, that evening, whenever), this catches up immediately instead of
// leaving positions open until the next trading day's cron ever fires
// (mirrors FillScheduledIfMarketOpen's identical boot-time catch-up for
// the market-open side).
func (s *Service) SquareOffIntradayIfPastCutoff(ctx context.Context) (int, error) {
	now := time.Now().In(market.IST)
	cutoff := time.Date(now.Year(), now.Month(), now.Day(), intradayCutoffHour, intradayCutoffMin, 0, 0, market.IST)
	if now.Before(cutoff) {
		return 0, nil
	}
	return s.SquareOffIntraday(ctx)
}
