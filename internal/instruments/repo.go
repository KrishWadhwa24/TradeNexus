// Package instruments is the repository for tradable instruments (populated
// from the Angel scrip master) and powers the watchlist autocomplete search.
package instruments

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when an instrument id doesn't exist.
var ErrNotFound = errors.New("instrument not found")

// Instrument is a tradable symbol.
type Instrument struct {
	ID            int64  `json:"id"`
	SymbolToken   string `json:"symbol_token"`
	Exchange      string `json:"exchange"`
	TradingSymbol string `json:"trading_symbol"`
	Name          string `json:"name"`
	LotSize       int    `json:"lot_size"`
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
			INSERT INTO instruments (symbol_token, exchange, trading_symbol, name, lot_size, active, updated_at)
			VALUES ($1,$2,$3,$4,$5,TRUE, now())
			ON CONFLICT (exchange, symbol_token) DO UPDATE
			SET trading_symbol = EXCLUDED.trading_symbol,
			    name           = EXCLUDED.name,
			    lot_size       = EXCLUDED.lot_size,
			    active         = TRUE,
			    updated_at     = now()`,
			it.SymbolToken, it.Exchange, it.TradingSymbol, it.Name, it.LotSize)
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
	err := r.pool.QueryRow(ctx, `
		SELECT id, symbol_token, exchange, trading_symbol, name, lot_size
		FROM instruments WHERE id = $1`, id).
		Scan(&it.ID, &it.SymbolToken, &it.Exchange, &it.TradingSymbol, &it.Name, &it.LotSize)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	return it, err
}
