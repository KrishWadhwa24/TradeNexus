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
func (r *Repo) TrackedUnderlyings(ctx context.Context) ([]instruments.Instrument, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, symbol_token, exchange, trading_symbol, name, lot_size, underlying_symbol
		FROM instruments
		WHERE underlying_symbol IN ('NIFTY', 'SENSEX') AND option_type IS NULL AND active = TRUE`)
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
