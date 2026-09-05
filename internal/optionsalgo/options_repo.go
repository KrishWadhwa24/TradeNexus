package optionsalgo

import (
	"context"
	"errors"
	"fmt"
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

// OptionExpiryAtLeastDaysOut returns the soonest expiry at least minDays
// away — used instead of NearestOptionExpiry so the algo isn't structurally
// stuck buying the fastest-decaying contract available (theta accelerates
// hardest in an option's final days, making nearest-expiry the worst choice
// for a long-only buyer).
//
// Deliberately does NOT try to identify "the monthly contract" specifically
// (e.g. picking the last expiry within each calendar month) — the
// derivatives sync only pulls near-dated contracts, so a month with only
// one expiry synced so far would have that single weekly wrongly picked as
// "the monthly." Asking for "far enough out" sidesteps that entirely: it
// doesn't matter whether the result IS the monthly contract, only that it's
// past the high-decay near dates.
func (r *Repo) OptionExpiryAtLeastDaysOut(ctx context.Context, underlying string, minDays int) (time.Time, error) {
	var expiry time.Time
	err := r.pool.QueryRow(ctx, `
		SELECT MIN(expiry_date) FROM instruments
		WHERE underlying_symbol = $1 AND option_type IN ('CE','PE')
		  AND expiry_date >= CURRENT_DATE + ($2 * INTERVAL '1 day') AND active = TRUE`,
		underlying, minDays).Scan(&expiry)
	if err != nil {
		return time.Time{}, err
	}
	if expiry.IsZero() {
		return time.Time{}, fmt.Errorf("no option expiry at least %d days out — run the derivatives sync or lower MinDaysToExpiry", minDays)
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

// GetInstrumentExpiry returns one instrument's expiry date (nil for a
// non-option instrument) — used by the management tick's expiry-day
// force-exit check. Read-only borrow of the instruments table, same
// convention as everything else in this file.
func (r *Repo) GetInstrumentExpiry(ctx context.Context, instrumentID int64) (*time.Time, error) {
	var expiry *time.Time
	err := r.pool.QueryRow(ctx, `SELECT expiry_date FROM instruments WHERE id = $1`, instrumentID).Scan(&expiry)
	return expiry, err
}

// GetInstrumentExchangeToken returns one instrument's exchange + symbol
// token — used by the management tick to fetch a fresh live LTP
// (GetOptionQuoteFull) for a held position. Deliberately NOT relying on
// paper.Trades' CurrentPrice for this: that only resolves via the live-tick
// cache (populated only for instruments some client has actively
// subscribed, which nothing does for a purchased option contract) or daily
// candles (never stored for individual option contracts) — so it would be
// nil on essentially every tick for an algo position, silently disabling
// stop/trailing management. A direct Quote-FULL poll (same call already
// verified live in Phase 0) is cheap at the current "max 1 open position"
// scale and doesn't depend on some other part of the app happening to have
// the same contract open in a browser tab.
func (r *Repo) GetInstrumentExchangeToken(ctx context.Context, instrumentID int64) (exchange, token string, err error) {
	err = r.pool.QueryRow(ctx, `SELECT exchange, symbol_token FROM instruments WHERE id = $1`, instrumentID).Scan(&exchange, &token)
	return exchange, token, err
}
