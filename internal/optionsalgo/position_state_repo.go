package optionsalgo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// PositionState is the per-trade trailing-stop state that must persist
// across ticks (and now potentially many days, since algo positions are no
// longer force-closed same-day) — internal/paper's Trade has no room for
// this, and shouldn't: it's algo-strategy bookkeeping, not a paper-trading
// concern.
type PositionState struct {
	TradeID      int64
	HighestPrice float64
	CurrentStop  float64
	// MFE/MAE are the max favorable/adverse excursion seen over the
	// position's life, in price points (currentPrice - entryPrice) — MFE is
	// the best unrealized profit ever seen, MAE the worst unrealized loss
	// (a negative number). Logged to algo_decisions on exit, per the
	// script's explicit logging requirement.
	MFE float64
	MAE float64
}

// UpsertPositionState creates or updates the trailing state for one trade.
func (r *Repo) UpsertPositionState(ctx context.Context, s PositionState) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO algo_position_state (trade_id, highest_price, current_stop, mfe, mae, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (trade_id) DO UPDATE
		SET highest_price = EXCLUDED.highest_price, current_stop = EXCLUDED.current_stop,
		    mfe = EXCLUDED.mfe, mae = EXCLUDED.mae, updated_at = now()`,
		s.TradeID, s.HighestPrice, s.CurrentStop, s.MFE, s.MAE)
	return err
}

// GetPositionState returns the trailing state for one trade, and whether a
// row exists yet (false right after entry, before the first management tick
// has run).
func (r *Repo) GetPositionState(ctx context.Context, tradeID int64) (PositionState, bool, error) {
	var s PositionState
	s.TradeID = tradeID
	err := r.pool.QueryRow(ctx,
		`SELECT highest_price, current_stop, mfe, mae FROM algo_position_state WHERE trade_id = $1`,
		tradeID).Scan(&s.HighestPrice, &s.CurrentStop, &s.MFE, &s.MAE)
	if errors.Is(err, pgx.ErrNoRows) {
		return PositionState{}, false, nil // not initialized yet — normal right after entry
	}
	if err != nil {
		return PositionState{}, false, err
	}
	return s, true, nil
}

// DeletePositionState removes a closed trade's trailing state — no longer
// needed once the position is closed, and avoids algo_position_state growing
// unboundedly with rows for positions that will never be read again.
func (r *Repo) DeletePositionState(ctx context.Context, tradeID int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM algo_position_state WHERE trade_id = $1`, tradeID)
	return err
}
