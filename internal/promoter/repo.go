package promoter

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a trade row doesn't exist.
var ErrNotFound = errors.New("promoter trade not found")

// Repo persists tracked promoter/director/KMP trades and the set of
// already-inspected filing ids (so we don't re-download/re-parse an XBRL
// document we've already looked at).
type Repo struct{ pool *pgxpool.Pool }

// NewRepo builds the repository.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// FilterUnseen returns the subset of appIDs we have not yet inspected.
func (r *Repo) FilterUnseen(ctx context.Context, appIDs []int64) ([]int64, error) {
	if len(appIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT app_id FROM promoter_seen_filings WHERE app_id = ANY($1)`, appIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := make(map[int64]bool, len(appIDs))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		seen[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(appIDs))
	for _, id := range appIDs {
		if !seen[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

// MarkSeen records that a filing has been inspected, regardless of whether
// it produced any tracked trade.
func (r *Repo) MarkSeen(ctx context.Context, appID int64) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO promoter_seen_filings (app_id) VALUES ($1) ON CONFLICT (app_id) DO NOTHING`, appID)
	return err
}

// PruneSeenOlderThan deletes seen-filing markers older than cutoff — this
// table only needs to cover the poll's dedup window, not full history.
func (r *Repo) PruneSeenOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM promoter_seen_filings WHERE seen_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// InsertTrade inserts a tracked trade if it isn't already stored. Returns
// whether a row was actually inserted (false ⇒ duplicate, caller should not
// alert again).
func (r *Repo) InsertTrade(ctx context.Context, t Trade) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO promoter_trades
			(id, app_id, symbol, company_name, isin, person_name, category, event_type, mode,
			 quantity, value_inr, qty_before, pct_before, qty_after, pct_after,
			 trade_date_from, trade_date_to, regulation, filing_url, broadcast_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		ON CONFLICT (id) DO NOTHING`,
		t.ID, t.AppID, t.Symbol, t.CompanyName, t.ISIN, t.PersonName, t.Category, t.EventType, t.Mode,
		t.Quantity, t.Value, t.QtyBefore, t.PctBefore, t.QtyAfter, t.PctAfter,
		t.TradeFrom, t.TradeTo, t.Regulation, t.FilingURL, t.BroadcastAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// MarkAlerted records that a Telegram alert was sent for a trade.
func (r *Repo) MarkAlerted(ctx context.Context, id string, at time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE promoter_trades SET alerted=true, alerted_at=$2 WHERE id=$1`, id, at)
	return err
}

const selectCols = `id, app_id, symbol, company_name, isin, person_name, category, event_type, mode,
	quantity, value_inr, qty_before, pct_before, qty_after, pct_after,
	trade_date_from, trade_date_to, regulation, filing_url, broadcast_at, alerted, alerted_at`

func scanTrade(row pgx.Row) (Trade, error) {
	var t Trade
	err := row.Scan(&t.ID, &t.AppID, &t.Symbol, &t.CompanyName, &t.ISIN, &t.PersonName, &t.Category, &t.EventType, &t.Mode,
		&t.Quantity, &t.Value, &t.QtyBefore, &t.PctBefore, &t.QtyAfter, &t.PctAfter,
		&t.TradeFrom, &t.TradeTo, &t.Regulation, &t.FilingURL, &t.BroadcastAt, &t.Alerted, &t.AlertedAt)
	return t, err
}

// ListRecent returns tracked trades broadcast in the last `days` days,
// newest first.
func (r *Repo) ListRecent(ctx context.Context, days int) ([]Trade, error) {
	cutoff := time.Now().AddDate(0, 0, -days)
	rows, err := r.pool.Query(ctx, `SELECT `+selectCols+`
		FROM promoter_trades WHERE broadcast_at >= $1 ORDER BY broadcast_at DESC`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Trade
	for rows.Next() {
		t, err := scanTrade(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetByID returns one trade (for the admin "send alert" action).
func (r *Repo) GetByID(ctx context.Context, id string) (Trade, error) {
	t, err := scanTrade(r.pool.QueryRow(ctx, `SELECT `+selectCols+` FROM promoter_trades WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Trade{}, ErrNotFound
	}
	return t, err
}

// PruneOlderThan deletes trades broadcast before cutoff (retention).
func (r *Repo) PruneOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM promoter_trades WHERE broadcast_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
