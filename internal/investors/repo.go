package investors

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo persists tracked investor positions and the set of already-inspected
// SHP filing ids (so we don't re-download/re-parse an XBRL document we've
// already looked at) — same shape as promoter.Repo.
type Repo struct{ pool *pgxpool.Pool }

func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// FilterUnseen returns the subset of recordIDs we have not yet inspected.
func (r *Repo) FilterUnseen(ctx context.Context, recordIDs []string) ([]string, error) {
	if len(recordIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT record_id FROM investor_seen_filings WHERE record_id = ANY($1)`, recordIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := make(map[string]bool, len(recordIDs))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		seen[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(recordIDs))
	for _, id := range recordIDs {
		if !seen[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

// MarkSeen records that a filing has been inspected, regardless of whether
// it produced any tracked holding.
func (r *Repo) MarkSeen(ctx context.Context, recordID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO investor_seen_filings (record_id) VALUES ($1) ON CONFLICT (record_id) DO NOTHING`, recordID)
	return err
}

// PruneSeenOlderThan deletes seen-filing markers older than cutoff.
func (r *Repo) PruneSeenOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM investor_seen_filings WHERE seen_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// UpsertHolding stores a tracked investor's latest disclosed position in one
// stock. Idempotent by (investor_key, symbol): a filing whose report_date is
// not newer than what's already stored is a no-op, so re-processing an old
// filing (e.g. a wide catch-up window re-touching one we've since seen a
// fresher quarter for) can never regress a newer snapshot with a stale one.
// first_seen_date is only ever set on first insert.
func (r *Repo) UpsertHolding(ctx context.Context, investorKey string, h Holding) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO investor_positions
			(investor_key, symbol, investor_name, company_name, shares, pct_holding, report_date, first_seen_date, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$7,now())
		ON CONFLICT (investor_key, symbol) DO UPDATE SET
			investor_name = EXCLUDED.investor_name,
			company_name  = EXCLUDED.company_name,
			shares        = EXCLUDED.shares,
			pct_holding   = EXCLUDED.pct_holding,
			report_date   = EXCLUDED.report_date,
			updated_at    = now()
		WHERE investor_positions.report_date <= EXCLUDED.report_date`,
		investorKey, h.Symbol, h.InvestorName, h.CompanyName, h.Shares, h.PctHolding, h.ReportDate)
	return err
}

// RemoveStaleHoldings deletes any tracked investor's position in symbol
// whose stored report_date is older than reportDate and who isn't in
// stillHeldKeys (normalized investor keys) — i.e. investors this newer
// filing didn't name, meaning they've sold out or dropped below NSE's
// disclosure threshold since the last filing we saw for this stock.
//
// The report_date < reportDate guard is what makes this safe to call from a
// catch-up window that doesn't process filings in chronological order: an
// older filing being (re)processed can only ever compare against — and
// possibly delete — rows even older than itself, never a row a genuinely
// newer filing already produced. stillHeldKeys must be non-nil (even if
// empty) — a nil slice binds as SQL NULL, which makes "<> ALL(NULL)"
// evaluate to NULL (excluding every row) instead of the intended "delete
// everyone" when nobody matched this filing.
func (r *Repo) RemoveStaleHoldings(ctx context.Context, symbol string, reportDate time.Time, stillHeldKeys []string) (int64, error) {
	if stillHeldKeys == nil {
		stillHeldKeys = []string{}
	}
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM investor_positions
		WHERE symbol = $1 AND report_date < $2 AND investor_key <> ALL($3)`,
		symbol, reportDate, stillHeldKeys)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ListInvestors returns one summary row per tracked investor that currently
// has at least one disclosed holding, largest stock count first. Each row
// includes the investor's single largest stake (top_symbol/top_pct), picked
// via a per-investor rank over pct_holding.
func (r *Repo) ListInvestors(ctx context.Context) ([]InvestorSummary, error) {
	rows, err := r.pool.Query(ctx, `
		WITH ranked AS (
			SELECT investor_name, symbol, pct_holding,
				ROW_NUMBER() OVER (PARTITION BY investor_name ORDER BY pct_holding DESC) AS rn
			FROM investor_positions
		)
		SELECT p.investor_name, COUNT(*) AS stock_count, MAX(p.report_date) AS latest_date,
			r.symbol, r.pct_holding
		FROM investor_positions p
		JOIN ranked r ON r.investor_name = p.investor_name AND r.rn = 1
		GROUP BY p.investor_name, r.symbol, r.pct_holding
		ORDER BY stock_count DESC, p.investor_name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InvestorSummary
	for rows.Next() {
		var s InvestorSummary
		if err := rows.Scan(&s.InvestorName, &s.StockCount, &s.LatestDate, &s.TopSymbol, &s.TopPct); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

const holdingCols = `investor_name, symbol, company_name, shares, pct_holding, report_date, first_seen_date`

// HoldingsForInvestor returns every stock one tracked investor currently
// holds, largest stake first.
func (r *Repo) HoldingsForInvestor(ctx context.Context, investorKey string) ([]Holding, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+holdingCols+`
		FROM investor_positions
		WHERE investor_key = $1
		ORDER BY pct_holding DESC`, investorKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Holding
	for rows.Next() {
		var h Holding
		if err := rows.Scan(&h.InvestorName, &h.Symbol, &h.CompanyName, &h.Shares, &h.PctHolding, &h.ReportDate, &h.FirstSeenDate); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// HoldingsForSymbol returns every tracked investor currently holding one
// stock, largest stake first — the Stock 360 view's future hook (not wired
// up yet; that page lives on a branch not merged here).
func (r *Repo) HoldingsForSymbol(ctx context.Context, symbol string) ([]Holding, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+holdingCols+`
		FROM investor_positions
		WHERE symbol = $1
		ORDER BY pct_holding DESC`, symbol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Holding
	for rows.Next() {
		var h Holding
		if err := rows.Scan(&h.InvestorName, &h.Symbol, &h.CompanyName, &h.Shares, &h.PctHolding, &h.ReportDate, &h.FirstSeenDate); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
