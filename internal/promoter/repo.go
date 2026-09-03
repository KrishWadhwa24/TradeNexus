package promoter

import (
	"context"
	"errors"
	"sort"
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

// InsertTrade inserts a tracked trade if it isn't already stored (by its own
// id). Returns inserted (false ⇒ the exact same disclosure block was already
// stored — a retried/re-parsed filing, caller should not alert again) and
// duplicate (true ⇒ this is a genuinely new row, but NSE re-filed an
// already-tracked real-world transaction under a new app_id — same person/
// symbol/quantity/direction/date, see isDuplicateFiling — the caller should
// still not alert again or count it as new activity). A newly-inserted,
// non-duplicate trade also gets folded into that person's permanent position
// (see accumulatePosition) — this is the only place new trades enter the
// system, so it's the only place that needs to know about it.
func (r *Repo) InsertTrade(ctx context.Context, t Trade) (inserted, duplicate bool, err error) {
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
		return false, false, err
	}
	inserted = tag.RowsAffected() > 0
	if !inserted {
		return false, false, nil
	}
	duplicate, err = r.isDuplicateFiling(ctx, t)
	if err != nil {
		return true, false, err
	}
	if !duplicate {
		if err := r.accumulatePosition(ctx, t); err != nil {
			return true, false, err
		}
	}
	return true, duplicate, nil
}

// isDuplicateFiling reports whether another already-accumulated trade
// exists for the same (symbol, person, quantity, trade date, direction) as
// t. NSE sometimes files the same real-world transaction twice (e.g. under
// two disclosure paths) — same share count, same person, same day — which
// would otherwise double-count that one transaction's shares/value into the
// permanent position. t itself is already in promoter_trades by the time
// this runs, so the match excludes t.ID.
func (r *Repo) isDuplicateFiling(ctx context.Context, t Trade) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM promoter_trades
			WHERE id <> $1
			  AND symbol = $2
			  AND upper(btrim(person_name)) = upper(btrim($3))
			  AND quantity = $4
			  AND event_type = $5
			  AND COALESCE(trade_date_to, broadcast_at::date) = COALESCE($6::date, $7::date)
		)`,
		t.ID, t.Symbol, t.PersonName, t.Quantity, t.EventType, t.TradeTo, t.BroadcastAt,
	).Scan(&exists)
	return exists, err
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
// newest first. De-duplicates the same way ListForPerson does — NSE
// sometimes re-files an already-tracked real-world transaction under a new
// app_id (see isDuplicateFiling), and without this the same transaction
// would show up twice in the feed even though InsertTrade only alerts on it
// once.
func (r *Repo) ListRecent(ctx context.Context, days int) ([]Trade, error) {
	cutoff := time.Now().AddDate(0, 0, -days)
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (symbol, upper(btrim(person_name)), quantity, event_type, COALESCE(trade_date_to, broadcast_at::date))
			`+selectCols+`
		FROM promoter_trades
		WHERE broadcast_at >= $1
		ORDER BY symbol, upper(btrim(person_name)), quantity, event_type, COALESCE(trade_date_to, broadcast_at::date), created_at ASC`,
		cutoff)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BroadcastAt.After(out[j].BroadcastAt) })
	return out, nil
}

// ListForPerson returns one person's individual disclosures for one stock,
// newest first by each disclosure's own reported trade date — the
// per-transaction rate/qty/date history behind the permanent aggregate in
// promoter_positions. Bound by whatever's still in promoter_trades
// (PROMOTER_RETENTION_DAYS) — unlike the aggregate, this view can't see
// further back than that. Duplicate filings of the same real-world
// transaction (see isDuplicateFiling) are collapsed to one.
//
// This sorts by each row's own stated date, not the qty_before/qty_after
// chain used to pick promoter_positions' first/latest stake (see
// accumulatePosition) — NSE occasionally files a batch whose stated dates
// don't match the qty chain's true execution order, and for a per-
// transaction list the user is scanning by reported date, this shows
// exactly what NSE reported rather than a reordering that can look
// "unsorted" against the visible dates.
func (r *Repo) ListForPerson(ctx context.Context, symbol, personKey string) ([]Trade, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (quantity, event_type, COALESCE(trade_date_to, broadcast_at::date))
			`+selectCols+`
		FROM promoter_trades
		WHERE symbol = $1 AND upper(btrim(person_name)) = $2
		ORDER BY quantity, event_type, COALESCE(trade_date_to, broadcast_at::date), created_at ASC`,
		symbol, personKey)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		di, dj := tradeDate(out[i]), tradeDate(out[j])
		if !di.Equal(dj) {
			return di.After(dj)
		}
		return out[i].BroadcastAt.After(out[j].BroadcastAt)
	})
	return out, nil
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
