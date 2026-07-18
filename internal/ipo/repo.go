package ipo

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when an IPO row doesn't exist.
var ErrNotFound = errors.New("ipo not found")

// Repo persists the (open + upcoming only) IPO set.
type Repo struct{ pool *pgxpool.Pool }

// NewRepo builds the repository.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// Upsert inserts/updates one IPO. Signal state (signal_tier/signaled_at) is
// deliberately NOT overwritten here — it's owned by the signalling path.
func (r *Repo) Upsert(ctx context.Context, x IPO) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ipos
			(id, name, board, category, status, gmp, gmp_percent, subscription, price,
			 ipo_size, lot, pe, rating, open_date, close_date, boa_date, listing_date,
			 url, updated_on, last_polled)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19, now())
		ON CONFLICT (id) DO UPDATE SET
			name=EXCLUDED.name, board=EXCLUDED.board, category=EXCLUDED.category,
			status=EXCLUDED.status, gmp=EXCLUDED.gmp, gmp_percent=EXCLUDED.gmp_percent,
			subscription=EXCLUDED.subscription, price=EXCLUDED.price, ipo_size=EXCLUDED.ipo_size,
			lot=EXCLUDED.lot, pe=EXCLUDED.pe, rating=EXCLUDED.rating, open_date=EXCLUDED.open_date,
			close_date=EXCLUDED.close_date, boa_date=EXCLUDED.boa_date,
			listing_date=EXCLUDED.listing_date, url=EXCLUDED.url, updated_on=EXCLUDED.updated_on,
			last_polled=now()`,
		x.ID, x.Name, x.Board, x.Category, x.Status, x.GMP, x.GMPPercent, x.Subscription, x.Price,
		x.Size, x.Lot, x.PE, x.Rating, x.OpenDate, x.CloseDate, x.BoADate, x.ListingDate,
		x.URL, x.UpdatedOn)
	return err
}

// PruneExcept deletes every IPO whose id is not in keepIDs. With an empty slice
// it clears the table (used when nothing is open/upcoming anymore).
func (r *Repo) PruneExcept(ctx context.Context, keepIDs []int64) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM ipos WHERE NOT (id = ANY($1))`, keepIDs)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

const selectCols = `id, name, board, category, status, gmp, gmp_percent, subscription, price,
	ipo_size, lot, pe, rating, open_date, close_date, boa_date, listing_date, url, updated_on, signal_tier`

func scanIPO(row pgx.Row) (IPO, error) {
	var x IPO
	err := row.Scan(&x.ID, &x.Name, &x.Board, &x.Category, &x.Status, &x.GMP, &x.GMPPercent,
		&x.Subscription, &x.Price, &x.Size, &x.Lot, &x.PE, &x.Rating,
		&x.OpenDate, &x.CloseDate, &x.BoADate, &x.ListingDate, &x.URL, &x.UpdatedOn, &x.SignalTier)
	return x, err
}

// ListActive returns open + upcoming IPOs, open first, then by close date.
func (r *Repo) ListActive(ctx context.Context) ([]IPO, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+selectCols+`
		FROM ipos WHERE status IN ('open','upcoming')
		ORDER BY (status = 'open') DESC, close_date ASC NULLS LAST, gmp_percent DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IPO
	for rows.Next() {
		x, err := scanIPO(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// GetByID returns one IPO.
func (r *Repo) GetByID(ctx context.Context, id int64) (IPO, error) {
	x, err := scanIPO(r.pool.QueryRow(ctx, `SELECT `+selectCols+` FROM ipos WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return IPO{}, ErrNotFound
	}
	return x, err
}

// SetSignalTier records that a signal of the given tier was sent for an IPO.
func (r *Repo) SetSignalTier(ctx context.Context, id int64, tier string, at time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE ipos SET signal_tier=$2, signaled_at=$3 WHERE id=$1`, id, tier, at)
	return err
}
