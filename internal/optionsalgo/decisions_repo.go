package optionsalgo

import (
	"context"
	"time"
)

// Decision is one evaluation tick's full record — logged whether or not it
// traded, per the script's explicit requirement to log every no-trade day
// too, not just executed trades. Also used for exit events (Action="EXIT").
type Decision struct {
	ID               int64
	EvaluatedAt      time.Time
	NiftySpot        float64
	ORHigh, ORLow    float64
	VWAP             float64
	EMAFast, EMASlow float64
	ATR, ATRAvg      float64
	Direction        string
	DirectionReason  string

	SelectedSymbol  string
	SelectedStrike  float64
	SelectedDelta   float64
	SelectedIV      float64
	SelectedTheta   float64
	SelectionReason string
	EntryOK         bool
	EntryReason     string

	// Action is one of: NO_DIRECTION | DAILY_LOSS_LIMIT | WEEKLY_LOSS_LIMIT |
	// MAX_POSITIONS | MAX_TRADES_TODAY | NO_CONTRACT | ENTRY_REJECTED |
	// SIZED_ZERO | EXECUTED | ORDER_REJECTED | ERROR | EXIT
	Action  string
	TradeID *int64
	Detail  string

	// Exit-specific — only set when Action == "EXIT".
	ExitPrice  *float64
	ExitReason string
	PnL        *float64
	MFE        *float64
	MAE        *float64
}

// LogDecision inserts one row — never updated afterward, an append-only
// audit trail.
func (r *Repo) LogDecision(ctx context.Context, d Decision) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO algo_decisions (
			evaluated_at, nifty_spot, or_high, or_low, vwap, ema_fast, ema_slow, atr, atr_avg,
			direction, direction_reason,
			selected_symbol, selected_strike, selected_delta, selected_iv, selected_theta, selection_reason,
			entry_ok, entry_reason,
			action, trade_id, exit_price, exit_reason, pnl, mfe, mae, detail
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,
			$10,$11,
			$12,$13,$14,$15,$16,$17,
			$18,$19,
			$20,$21,$22,$23,$24,$25,$26,$27
		)`,
		d.EvaluatedAt, d.NiftySpot, d.ORHigh, d.ORLow, d.VWAP, d.EMAFast, d.EMASlow, d.ATR, d.ATRAvg,
		d.Direction, d.DirectionReason,
		d.SelectedSymbol, d.SelectedStrike, d.SelectedDelta, d.SelectedIV, d.SelectedTheta, d.SelectionReason,
		d.EntryOK, d.EntryReason,
		d.Action, d.TradeID, d.ExitPrice, d.ExitReason, d.PnL, d.MFE, d.MAE, d.Detail,
	)
	return err
}

// RecentDecisions returns the most recent decisions, newest first — powers
// the admin/Options-page decision-log view.
func (r *Repo) RecentDecisions(ctx context.Context, limit int) ([]Decision, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, evaluated_at, nifty_spot, or_high, or_low, vwap, ema_fast, ema_slow, atr, atr_avg,
		       direction, direction_reason,
		       selected_symbol, selected_strike, selected_delta, selected_iv, selected_theta, selection_reason,
		       entry_ok, entry_reason,
		       action, trade_id, exit_price, exit_reason, pnl, mfe, mae, detail
		FROM algo_decisions
		ORDER BY evaluated_at DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Decision
	for rows.Next() {
		var d Decision
		if err := rows.Scan(
			&d.ID, &d.EvaluatedAt, &d.NiftySpot, &d.ORHigh, &d.ORLow, &d.VWAP, &d.EMAFast, &d.EMASlow, &d.ATR, &d.ATRAvg,
			&d.Direction, &d.DirectionReason,
			&d.SelectedSymbol, &d.SelectedStrike, &d.SelectedDelta, &d.SelectedIV, &d.SelectedTheta, &d.SelectionReason,
			&d.EntryOK, &d.EntryReason,
			&d.Action, &d.TradeID, &d.ExitPrice, &d.ExitReason, &d.PnL, &d.MFE, &d.MAE, &d.Detail,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
