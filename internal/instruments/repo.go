// Package instruments is the repository for tradable instruments (populated
// from the Angel scrip master) and powers the watchlist autocomplete search.
package instruments

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when an instrument id doesn't exist.
var ErrNotFound = errors.New("instrument not found")

// Instrument is a tradable symbol. The four option fields are populated only
// for NFO/BFO derivative rows (index options) and for index-spot rows
// (UnderlyingSymbol only, e.g. "NIFTY" on the Nifty 50 index instrument
// itself); a plain equity leaves all four nil/empty.
type Instrument struct {
	ID               int64      `json:"id"`
	SymbolToken      string     `json:"symbol_token"`
	Exchange         string     `json:"exchange"`
	TradingSymbol    string     `json:"trading_symbol"`
	Name             string     `json:"name"`
	LotSize          int        `json:"lot_size"`
	StrikePrice      *float64   `json:"strike_price,omitempty"`
	ExpiryDate       *time.Time `json:"expiry_date,omitempty"`
	OptionType       string     `json:"option_type,omitempty"`
	UnderlyingSymbol string     `json:"underlying_symbol,omitempty"`
}

// Repo is the instruments datastore.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo builds the repository.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// BulkUpsert inserts/updates instruments keyed on (exchange, symbol_token).
func (r *Repo) BulkUpsert(ctx context.Context, items []Instrument) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}
	batch := &pgx.Batch{}
	for _, it := range items {
		batch.Queue(`
			INSERT INTO instruments
				(symbol_token, exchange, trading_symbol, name, lot_size,
				 strike_price, expiry_date, option_type, underlying_symbol, active, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,TRUE, now())
			ON CONFLICT (exchange, symbol_token) DO UPDATE
			SET trading_symbol    = EXCLUDED.trading_symbol,
			    name              = EXCLUDED.name,
			    lot_size          = EXCLUDED.lot_size,
			    strike_price      = EXCLUDED.strike_price,
			    expiry_date       = EXCLUDED.expiry_date,
			    option_type       = EXCLUDED.option_type,
			    underlying_symbol = EXCLUDED.underlying_symbol,
			    active            = TRUE,
			    updated_at        = now()`,
			it.SymbolToken, it.Exchange, it.TradingSymbol, it.Name, it.LotSize,
			it.StrikePrice, it.ExpiryDate, nullIfEmpty(it.OptionType), nullIfEmpty(it.UnderlyingSymbol))
	}
	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range items {
		if _, err := br.Exec(); err != nil {
			return 0, fmt.Errorf("upsert instruments: %w", err)
		}
	}
	return len(items), nil
}

// nullIfEmpty binds an empty string as SQL NULL rather than "" — used for the
// option-only text columns so a plain equity's row reads as NULL, not "".
func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// DeactivateExpired marks any option contract past its expiry_date inactive
// — nothing else touches this, since equities never expire and everything
// else leaves expiry_date NULL.
func (r *Repo) DeactivateExpired(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE instruments SET active = FALSE, updated_at = now()
		WHERE expiry_date IS NOT NULL AND expiry_date < CURRENT_DATE AND active = TRUE`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Search returns active instruments whose trading symbol starts with q, or whose
// name contains q (case-insensitive). Powers the watchlist autocomplete.
func (r *Repo) Search(ctx context.Context, q string, limit int) ([]Instrument, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return []Instrument{}, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, symbol_token, exchange, trading_symbol, name, lot_size
		FROM instruments
		WHERE active = TRUE
		  AND (lower(trading_symbol) LIKE $1 OR lower(name) LIKE $2)
		ORDER BY (lower(trading_symbol) LIKE $1) DESC, trading_symbol
		LIMIT $3`,
		q+"%", "%"+q+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Instrument
	for rows.Next() {
		var it Instrument
		if err := rows.Scan(&it.ID, &it.SymbolToken, &it.Exchange, &it.TradingSymbol, &it.Name, &it.LotSize); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// GetByID fetches one instrument.
func (r *Repo) GetByID(ctx context.Context, id int64) (Instrument, error) {
	var it Instrument
	var optionType, underlyingSymbol *string
	err := r.pool.QueryRow(ctx, `
		SELECT id, symbol_token, exchange, trading_symbol, name, lot_size,
			strike_price, expiry_date, option_type, underlying_symbol
		FROM instruments WHERE id = $1`, id).
		Scan(&it.ID, &it.SymbolToken, &it.Exchange, &it.TradingSymbol, &it.Name, &it.LotSize,
			&it.StrikePrice, &it.ExpiryDate, &optionType, &underlyingSymbol)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	if optionType != nil {
		it.OptionType = *optionType
	}
	if underlyingSymbol != nil {
		it.UnderlyingSymbol = *underlyingSymbol
	}
	return it, err
}
