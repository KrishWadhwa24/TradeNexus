package optionsalgo

import (
	"context"
	"errors"
	"time"

	"tradenexus/internal/instruments"
)

// NearestOptionExpiry returns the soonest unexpired expiry for one
// underlying's option chain (already populated by the existing derivatives
// sync, internal/instruments/derivatives.go).
func (r *Repo) NearestOptionExpiry(ctx context.Context, underlying string) (time.Time, error) {
	var expiry time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT MIN(expiry_date) FROM instruments
		WHERE underlying_symbol = $1 AND option_type IN ('CE','PE')
		  AND expiry_date >= CURRENT_DATE AND active = TRUE`, underlying).Scan(&expiry)
	if err != nil {
		return time.Time{}, err
	}
	if expiry.IsZero() {
		return time.Time{}, errors.New("no unexpired option expiry found — run the derivatives sync")
	}
	return expiry, nil
}

// StrikesForExpiry returns every distinct strike available for one
// underlying+expiry, ascending — the pool NearestATMStrikes picks from.
func (r *Repo) StrikesForExpiry(ctx context.Context, underlying string, expiry time.Time) ([]float64, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT strike_price FROM instruments
		WHERE underlying_symbol = $1 AND option_type IN ('CE','PE')
		  AND expiry_date = $2 AND active = TRUE
		ORDER BY strike_price ASC`, underlying, expiry)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []float64
	for rows.Next() {
		var s float64
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// OptionContractsForStrikes returns the CE+PE instrument rows for one
// underlying+expiry restricted to the given strikes — the exact set
// NearestATMStrikes selected, joined against the real contracts (token, lot
// size) needed to actually price and trade them.
func (r *Repo) OptionContractsForStrikes(ctx context.Context, underlying string, expiry time.Time, strikes []float64) ([]instruments.Instrument, error) {
	if len(strikes) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, symbol_token, exchange, trading_symbol, name, lot_size,
		       strike_price, expiry_date, option_type, underlying_symbol
		FROM instruments
		WHERE underlying_symbol = $1 AND option_type IN ('CE','PE')
		  AND expiry_date = $2 AND strike_price = ANY($3) AND active = TRUE
		ORDER BY strike_price ASC, option_type ASC`, underlying, expiry, strikes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []instruments.Instrument
	for rows.Next() {
		var it instruments.Instrument
		if err := rows.Scan(&it.ID, &it.SymbolToken, &it.Exchange, &it.TradingSymbol, &it.Name, &it.LotSize,
			&it.StrikePrice, &it.ExpiryDate, &it.OptionType, &it.UnderlyingSymbol); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
