package deals

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo persists raw deal rows and the per-(type,date,symbol) alert ledger.
type Repo struct{ pool *pgxpool.Pool }

// NewRepo builds the repository.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// InsertRows stores raw deal rows idempotently (duplicates from a re-fetched
// day are ignored). Returns how many new rows were actually inserted. Each
// genuinely new mutual-fund row also gets folded into that fund's permanent
// position (see accumulateFundPosition) — this is the only place new deal
// rows enter the system, so it's the only place that needs to know about it.
func (r *Repo) InsertRows(ctx context.Context, rows []Row) (int, error) {
	inserted := 0
	for _, row := range rows {
		var newID int64
		err := r.pool.QueryRow(ctx, `
			INSERT INTO market_deals
				(deal_type, deal_date, symbol, security_name, client_name, buy_sell, quantity, price, remarks)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (deal_type, deal_date, symbol, client_name, buy_sell, quantity, price) DO NOTHING
			RETURNING id`,
			row.Type, row.Date, row.Symbol, row.SecurityName, row.ClientName, row.Side, row.Quantity, row.Price, row.Remarks,
		).Scan(&newID)
		if err != nil {
			if err == pgx.ErrNoRows {
				continue // exact duplicate of an existing row (same deal_type + raw client_name too) — nothing to do
			}
			return inserted, err
		}
		inserted++
		if isMutualFund(row.ClientName) {
			dup, err := r.isDuplicateFundTrade(ctx, newID, row)
			if err != nil {
				return inserted, err
			}
			if !dup {
				if err := r.accumulateFundPosition(ctx, row); err != nil {
					return inserted, err
				}
			}
		}
	}
	return inserted, nil
}

// rowsFor returns raw rows for a deal type, optionally filtered to one symbol,
// on/after cutoff, newest first.
func (r *Repo) rowsFor(ctx context.Context, t Type, symbol string, cutoff time.Time) ([]Row, error) {
	q := `SELECT deal_type, deal_date, symbol, security_name, client_name, buy_sell, quantity, price, remarks
		FROM market_deals WHERE deal_type = $1 AND deal_date >= $2`
	args := []any{t, cutoff}
	if symbol != "" {
		q += ` AND symbol = $3`
		args = append(args, symbol)
	}
	q += ` ORDER BY deal_date DESC, symbol`
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var x Row
		if err := rows.Scan(&x.Type, &x.Date, &x.Symbol, &x.SecurityName, &x.ClientName,
			&x.Side, &x.Quantity, &x.Price, &x.Remarks); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// RowsInWindow returns all rows for a deal type within the last `days` days.
func (r *Repo) RowsInWindow(ctx context.Context, t Type, days int) ([]Row, error) {
	return r.rowsFor(ctx, t, "", cutoffDays(days))
}

// RowsForSymbol returns rows for one symbol within the last `days` days.
func (r *Repo) RowsForSymbol(ctx context.Context, t Type, symbol string, days int) ([]Row, error) {
	return r.rowsFor(ctx, t, symbol, cutoffDays(days))
}


// AlreadyAlerted reports whether a (type, date, symbol) has been alerted.
func (r *Repo) AlreadyAlerted(ctx context.Context, t Type, day time.Time, symbol string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM market_deals_alerted
			WHERE deal_type = $1 AND deal_date = $2 AND symbol = $3)`,
		t, dateOnly(day), symbol).Scan(&exists)
	return exists, err
}

// MarkAlerted records that a (type, date, symbol) has been alerted.
func (r *Repo) MarkAlerted(ctx context.Context, t Type, day time.Time, symbol string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO market_deals_alerted (deal_type, deal_date, symbol) VALUES ($1,$2,$3)
		ON CONFLICT (deal_type, deal_date, symbol) DO NOTHING`,
		t, dateOnly(day), symbol)
	return err
}

// AlertMarker is one row of the sent-alert ledger.
type AlertMarker struct {
	Symbol    string
	DealDate  time.Time
	AlertedAt time.Time
}

// ListAlerted returns the sent-alert ledger for a deal type within the last
// `days` days, most recently sent first.
func (r *Repo) ListAlerted(ctx context.Context, t Type, days int) ([]AlertMarker, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT symbol, deal_date, alerted_at FROM market_deals_alerted
		WHERE deal_type = $1 AND deal_date >= $2 ORDER BY alerted_at DESC`,
		t, cutoffDays(days))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AlertMarker
	for rows.Next() {
		var m AlertMarker
		if err := rows.Scan(&m.Symbol, &m.DealDate, &m.AlertedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// PruneOlderThan deletes deal rows and alert markers older than cutoff.
func (r *Repo) PruneOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	c := dateOnly(cutoff)
	tag, err := r.pool.Exec(ctx, `DELETE FROM market_deals WHERE deal_date < $1`, c)
	if err != nil {
		return 0, err
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM market_deals_alerted WHERE deal_date < $1`, c); err != nil {
		return tag.RowsAffected(), err
	}
	return tag.RowsAffected(), nil
}

func cutoffDays(days int) time.Time {
	if days <= 0 {
		days = 30
	}
	return dateOnly(time.Now().AddDate(0, 0, -days))
}
