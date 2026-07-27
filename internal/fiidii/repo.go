package fiidii

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo persists the per-trade-date auto-alert ledger, so a process restart
// never re-sends an alert that already went out for that date.
type Repo struct{ pool *pgxpool.Pool }

// NewRepo builds the repository.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// AlreadyAlerted reports whether the auto alert has already been sent for day.
func (r *Repo) AlreadyAlerted(ctx context.Context, day time.Time) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM fiidii_alerted WHERE trade_date = $1)`,
		day).Scan(&exists)
	return exists, err
}

// MarkAlerted records that the auto alert has been sent for day.
func (r *Repo) MarkAlerted(ctx context.Context, day time.Time) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO fiidii_alerted (trade_date) VALUES ($1)
		ON CONFLICT (trade_date) DO NOTHING`,
		day)
	return err
}
