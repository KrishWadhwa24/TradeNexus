package instruments

import (
	"context"
	"errors"
	"fmt"
)

// MaxFeatured caps the admin-curated "featured stocks" list shown on the
// public landing page — a short, deliberately curated set, not a watchlist.
const MaxFeatured = 10

// ErrFeaturedFull is returned when adding a stock would exceed MaxFeatured.
var ErrFeaturedFull = errors.New("featured stocks list is full")

// ErrAlreadyFeatured is returned when the instrument is already on the list.
var ErrAlreadyFeatured = errors.New("stock is already featured")

// ListFeatured returns the admin-curated featured stocks, in display order.
func (r *Repo) ListFeatured(ctx context.Context) ([]Instrument, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT i.id, i.symbol_token, i.exchange, i.trading_symbol, i.name, i.lot_size
		FROM featured_stocks f
		JOIN instruments i ON i.id = f.instrument_id
		ORDER BY f.rank, f.added_at`)
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

// AddFeatured appends an instrument to the featured list (ranked after the
// current last entry), rejecting once MaxFeatured is reached. Checks
// duplicate-membership before capacity, so re-adding an existing member of an
// already-full list reports ErrAlreadyFeatured, not ErrFeaturedFull.
func (r *Repo) AddFeatured(ctx context.Context, instrumentID int64) error {
	var exists bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM featured_stocks WHERE instrument_id = $1)`, instrumentID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrAlreadyFeatured
	}

	var count, maxRank int
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*), COALESCE(max(rank), -1) FROM featured_stocks`).Scan(&count, &maxRank); err != nil {
		return err
	}
	if count >= MaxFeatured {
		return ErrFeaturedFull
	}
	// rank = max(rank)+1, not count — count() collides with a still-in-use
	// rank once a removal has left a gap (e.g. 9 rows with ranks 0-7,9 after
	// removing rank 8: count()=9 would re-assign the already-used rank 9).
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO featured_stocks (instrument_id, rank) VALUES ($1, $2)`, instrumentID, maxRank+1); err != nil {
		return fmt.Errorf("add featured stock: %w", err)
	}
	return nil
}

// RemoveFeatured drops an instrument from the featured list.
func (r *Repo) RemoveFeatured(ctx context.Context, instrumentID int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM featured_stocks WHERE instrument_id = $1`, instrumentID)
	return err
}
