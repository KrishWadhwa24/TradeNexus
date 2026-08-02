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

// UpsertFlow stores (or refreshes, if NSE revises a published day) one day's
// DII/FII buy/sell/net, so weekly/monthly trend can be computed later.
func (r *Repo) UpsertFlow(ctx context.Context, day time.Time, snap Snapshot) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO fiidii_flows (trade_date, dii_buy, dii_sell, dii_net, fii_buy, fii_sell, fii_net)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (trade_date) DO UPDATE SET
			dii_buy = EXCLUDED.dii_buy, dii_sell = EXCLUDED.dii_sell, dii_net = EXCLUDED.dii_net,
			fii_buy = EXCLUDED.fii_buy, fii_sell = EXCLUDED.fii_sell, fii_net = EXCLUDED.fii_net,
			fetched_at = now()`,
		day, snap.DII.BuyValue, snap.DII.SellValue, snap.DII.NetValue,
		snap.FII.BuyValue, snap.FII.SellValue, snap.FII.NetValue)
	return err
}

// PeriodFlow is DII/FII flow summed over one week or month.
type PeriodFlow struct {
	PeriodStart time.Time `json:"period_start"`
	DII         Flow      `json:"dii"`
	FII         Flow      `json:"fii"`
}

// ListWeekly returns DII/FII flow summed per ISO week, oldest first, over the
// last `weeks` weeks (including the current, partial one).
func (r *Repo) ListWeekly(ctx context.Context, weeks int) ([]PeriodFlow, error) {
	return r.listByPeriod(ctx, "week", weeks)
}

// ListMonthly returns DII/FII flow summed per calendar month, oldest first,
// over the last `months` months (including the current, partial one).
func (r *Repo) ListMonthly(ctx context.Context, months int) ([]PeriodFlow, error) {
	return r.listByPeriod(ctx, "month", months)
}

func (r *Repo) listByPeriod(ctx context.Context, unit string, count int) ([]PeriodFlow, error) {
	if count <= 0 {
		count = 12
	}
	cutoff := time.Now().AddDate(0, 0, -count*7)
	if unit == "month" {
		cutoff = time.Now().AddDate(0, -count, 0)
	}
	rows, err := r.pool.Query(ctx, `
		SELECT date_trunc($1, trade_date) AS period,
		       SUM(dii_buy), SUM(dii_sell), SUM(dii_net),
		       SUM(fii_buy), SUM(fii_sell), SUM(fii_net)
		FROM fiidii_flows
		WHERE trade_date >= $2
		GROUP BY period
		ORDER BY period`,
		unit, dateOnly(cutoff))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PeriodFlow
	for rows.Next() {
		var p PeriodFlow
		if err := rows.Scan(&p.PeriodStart, &p.DII.BuyValue, &p.DII.SellValue, &p.DII.NetValue,
			&p.FII.BuyValue, &p.FII.SellValue, &p.FII.NetValue); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
