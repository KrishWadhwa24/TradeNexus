package optionsalgo

import (
	"context"

	"tradenexus/internal/instruments"
)

// TrackedUnderlyings returns the index-spot instruments this algo tracks —
// currently Nifty 50 and SENSEX. Looked up by the same underlying_symbol/
// option_type=NULL shape used when they were ingested (see
// internal/angel/scripmaster.go's indexSpotTokens), not hardcoded instrument
// IDs, so this keeps working after any resync without a code change.
// expiry_date IS NULL is required too — since NIFTY futures were added
// (option_type is also NULL for a future, see TrackedFutures), that's now
// the only thing distinguishing a true spot row from a future for the same
// underlying_symbol.
func (r *Repo) TrackedUnderlyings(ctx context.Context) ([]instruments.Instrument, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, symbol_token, exchange, trading_symbol, name, lot_size, underlying_symbol
		FROM instruments
		WHERE underlying_symbol IN ('NIFTY', 'SENSEX') AND option_type IS NULL
		  AND expiry_date IS NULL AND active = TRUE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []instruments.Instrument
	for rows.Next() {
		var it instruments.Instrument
		if err := rows.Scan(&it.ID, &it.SymbolToken, &it.Exchange, &it.TradingSymbol, &it.Name, &it.LotSize, &it.UnderlyingSymbol); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// TrackedFutures returns the nearest-expiry NIFTY future — tracked purely as
// a real-volume proxy for VWAP (NIFTY spot always reports 0 volume,
// confirmed live; an index has no real trade volume of its own). Separate
// query from TrackedUnderlyings, not a variant of it: a future is
// distinguished from the underlying's own index-spot row by having a
// non-null expiry_date (see internal/instruments/derivatives.go's FUTIDX
// branch), so this can't accidentally match the spot row or vice versa.
// NIFTY-only for now — SENSEX was evaluated and deferred (Angel's Option
// Greeks API doesn't cover it), see the options-algo plan.
func (r *Repo) TrackedFutures(ctx context.Context) ([]instruments.Instrument, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, symbol_token, exchange, trading_symbol, name, lot_size, underlying_symbol
		FROM instruments
		WHERE underlying_symbol = 'NIFTY' AND option_type IS NULL AND expiry_date IS NOT NULL AND active = TRUE
		ORDER BY expiry_date ASC
		LIMIT 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []instruments.Instrument
	for rows.Next() {
		var it instruments.Instrument
		if err := rows.Scan(&it.ID, &it.SymbolToken, &it.Exchange, &it.TradingSymbol, &it.Name, &it.LotSize, &it.UnderlyingSymbol); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
