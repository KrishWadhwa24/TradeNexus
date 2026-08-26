package promoter

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// accumulatePosition folds one newly-inserted trade into that person's
// permanent (person_key, symbol) position. Only ever called from InsertTrade
// for a trade that was just actually inserted, so each disclosure is folded
// in exactly once, ever — the position keeps growing/shrinking even after
// promoter_trades' own retention prune deletes the row that fed it.
//
// "First"/"latest" stake % are picked using the qty_before/qty_after chain,
// not each disclosure's self-reported date: NSE sometimes files a batch of
// disclosures whose stated trade_date_to values don't match the true
// execution order implied by the running shareholding count (one
// disclosure's qty_after must equal the next one's qty_before — that's
// forced by the bookkeeping and can't lie the way a reporting date can).
// first_qty_before/latest_qty_after track the chain endpoints we've seen so
// far; a new trade that chains onto either end updates the % it carries. A
// trade that doesn't obviously chain (a gap — e.g. its neighbor hasn't been
// ingested yet) falls back to the date-based heuristic for the % only.
//
// first_date/latest_date are deliberately kept as plain MIN/MAX over every
// disclosure's own date, independent of which one won the % selection above
// — "first/last disclosure seen" is a factual question (when did we last
// hear from NSE about this position), not an analytical one, and showing a
// date that doesn't match its own literal MIN/MAX would look wrong even
// though the % next to it is correct.
func (r *Repo) accumulatePosition(ctx context.Context, t Trade) error {
	key := normalizePersonKey(t.PersonName)
	date := tradeDate(t)
	var buyQty, sellQty int64
	var buyValue, sellValue float64
	if t.EventType == EventPromoterBuy || t.EventType == EventKMPBuy {
		buyQty, buyValue = t.Quantity, t.Value
	} else {
		sellQty, sellValue = t.Quantity, t.Value
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var firstPct, latestPct float64
	var firstDate, latestDate time.Time
	var firstQtyBefore, latestQtyAfter int64
	err = tx.QueryRow(ctx, `
		SELECT first_pct, first_date, first_qty_before, latest_pct, latest_date, latest_qty_after
		FROM promoter_positions WHERE person_key=$1 AND symbol=$2 FOR UPDATE`,
		key, t.Symbol).Scan(&firstPct, &firstDate, &firstQtyBefore, &latestPct, &latestDate, &latestQtyAfter)

	if errors.Is(err, pgx.ErrNoRows) {
		if _, err = tx.Exec(ctx, `
			INSERT INTO promoter_positions
				(person_key, symbol, person_name, company_name, category,
				 buy_qty, sell_qty, buy_value, sell_value,
				 first_pct, first_date, first_qty_before,
				 latest_pct, latest_date, latest_qty_after, disclosure_count, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$11,$14,1,now())`,
			key, t.Symbol, t.PersonName, t.CompanyName, t.Category,
			buyQty, sellQty, buyValue, sellValue,
			t.PctBefore, date, t.QtyBefore, t.PctAfter, t.QtyAfter); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}

	newFirstDate := firstDate
	if date.Before(firstDate) {
		newFirstDate = date
	}
	newLatestDate := latestDate
	if date.After(latestDate) {
		newLatestDate = date
	}

	// Chain match is authoritative for which %/qty wins; the date-based
	// comparison (against the OLD first/latest date, before this trade)
	// only decides the % in the no-chain-match fallback.
	newFirstPct, newFirstQty := firstPct, firstQtyBefore
	if t.QtyAfter == firstQtyBefore || date.Before(firstDate) {
		newFirstPct, newFirstQty = t.PctBefore, t.QtyBefore
	}

	newLatestPct, newLatestQty := latestPct, latestQtyAfter
	latestChanged := false
	if t.QtyBefore == latestQtyAfter || date.After(latestDate) {
		newLatestPct, newLatestQty = t.PctAfter, t.QtyAfter
		latestChanged = true
	}

	if _, err = tx.Exec(ctx, `
		UPDATE promoter_positions SET
			person_name = $3, company_name = $4,
			buy_qty = buy_qty + $5, sell_qty = sell_qty + $6,
			buy_value = buy_value + $7, sell_value = sell_value + $8,
			first_pct = $9, first_date = $10, first_qty_before = $11,
			latest_pct = $12, latest_date = $13, latest_qty_after = $14,
			category = CASE WHEN $15 THEN $16 ELSE category END,
			disclosure_count = disclosure_count + 1,
			updated_at = now()
		WHERE person_key=$1 AND symbol=$2`,
		key, t.Symbol, t.PersonName, t.CompanyName,
		buyQty, sellQty, buyValue, sellValue,
		newFirstPct, newFirstDate, newFirstQty,
		newLatestPct, newLatestDate, newLatestQty,
		latestChanged, t.Category); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListStockPositions returns one summary row per symbol, largest combined
// promoter/KMP point-increase first.
func (r *Repo) ListStockPositions(ctx context.Context) ([]StockSummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT symbol,
			MAX(company_name) AS company_name,
			COUNT(*) AS person_count,
			SUM(first_pct) AS first_pct,
			SUM(latest_pct) AS latest_pct,
			SUM(buy_value) AS buy_value,
			SUM(sell_value) AS sell_value,
			MAX(latest_date) AS latest_date
		FROM promoter_positions
		GROUP BY symbol
		ORDER BY (SUM(latest_pct) - SUM(first_pct)) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StockSummary
	for rows.Next() {
		var s StockSummary
		if err := rows.Scan(&s.Symbol, &s.CompanyName, &s.PersonCount, &s.FirstPct, &s.LatestPct,
			&s.BuyValue, &s.SellValue, &s.LatestDate); err != nil {
			return nil, err
		}
		s.PointIncrease = s.LatestPct - s.FirstPct
		out = append(out, s)
	}
	return out, rows.Err()
}

// PersonPositionsForSymbol returns every tracked person's position for one
// symbol, largest point-increase first.
func (r *Repo) PersonPositionsForSymbol(ctx context.Context, symbol string) ([]PersonPosition, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT person_name, category, buy_qty, sell_qty, buy_value, sell_value,
			first_pct, first_date, latest_pct, latest_date, disclosure_count
		FROM promoter_positions
		WHERE symbol = $1
		ORDER BY (latest_pct - first_pct) DESC`, symbol)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PersonPosition
	for rows.Next() {
		var p PersonPosition
		if err := rows.Scan(&p.PersonName, &p.Category, &p.BuyQty, &p.SellQty, &p.BuyValue, &p.SellValue,
			&p.FirstPct, &p.FirstDate, &p.LatestPct, &p.LatestDate, &p.DisclosureCount); err != nil {
			return nil, err
		}
		p.PointIncrease = p.LatestPct - p.FirstPct
		p.RelativeIncreasePct = relativeIncrease(p.FirstPct, p.LatestPct)
		out = append(out, p)
	}
	return out, rows.Err()
}

// BackfillPositions (re)computes first_pct/latest_pct (and their qty-chain
// endpoints) for every (person, symbol) pair currently in promoter_trades,
// and seeds a position row for any pair that doesn't have one yet.
//
// The "first"/"latest" endpoint fields are always safe to overwrite — they
// self-correct given whatever's currently visible in promoter_trades (see
// accumulatePosition for why qty-chain endpoints, not raw dates, decide
// them) — but buy_qty/sell_qty/buy_value/sell_value/disclosure_count are
// only ever additively accumulated by accumulatePosition and may already
// reflect history that's since aged out of promoter_trades' retention
// window, so this never touches them for a position that already exists;
// ON CONFLICT DO UPDATE only overwrites the endpoint fields, not the
// aggregates. Safe to run more than once.
func (r *Repo) BackfillPositions(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `
		WITH dedup AS (
			-- NSE sometimes files the same real-world transaction twice (same
			-- person, stock, share count, day, direction) under two disclosure
			-- paths. Collapse those to one row (earliest created) before
			-- aggregating, or the duplicate's shares/value get double-counted.
			SELECT DISTINCT ON (
				symbol, upper(btrim(person_name)), quantity, event_type,
				COALESCE(trade_date_to, broadcast_at::date)
			) *
			FROM promoter_trades
			ORDER BY
				symbol, upper(btrim(person_name)), quantity, event_type,
				COALESCE(trade_date_to, broadcast_at::date), created_at ASC
		),
		chain_ends AS (
			-- A row is the true chain start if no sibling disclosure's
			-- qty_after feeds into it, and the true chain end if no sibling's
			-- qty_before continues from it — the qty chain, unlike each
			-- disclosure's self-reported date, can't be filed out of order.
			SELECT d.*,
				NOT EXISTS (
					SELECT 1 FROM dedup d2
					WHERE d2.symbol = d.symbol AND upper(btrim(d2.person_name)) = upper(btrim(d.person_name))
					  AND d2.qty_after = d.qty_before
				) AS is_chain_start,
				NOT EXISTS (
					SELECT 1 FROM dedup d2
					WHERE d2.symbol = d.symbol AND upper(btrim(d2.person_name)) = upper(btrim(d.person_name))
					  AND d2.qty_before = d.qty_after
				) AS is_chain_end
			FROM dedup d
		)
		INSERT INTO promoter_positions
			(person_key, symbol, person_name, company_name, category,
			 buy_qty, sell_qty, buy_value, sell_value,
			 first_pct, first_date, first_qty_before,
			 latest_pct, latest_date, latest_qty_after, disclosure_count, updated_at)
		SELECT
			upper(btrim(person_name)) AS person_key,
			symbol,
			(array_agg(person_name ORDER BY COALESCE(trade_date_to, broadcast_at::date) DESC))[1],
			(array_agg(company_name ORDER BY COALESCE(trade_date_to, broadcast_at::date) DESC))[1],
			(array_agg(category ORDER BY COALESCE(trade_date_to, broadcast_at::date) DESC))[1],
			COALESCE(SUM(quantity) FILTER (WHERE event_type IN ('promoter_buy','kmp_buy')), 0),
			COALESCE(SUM(quantity) FILTER (WHERE event_type IN ('promoter_sell','kmp_sell')), 0),
			COALESCE(SUM(value_inr) FILTER (WHERE event_type IN ('promoter_buy','kmp_buy')), 0),
			COALESCE(SUM(value_inr) FILTER (WHERE event_type IN ('promoter_sell','kmp_sell')), 0),
			-- %/qty use the unambiguous chain start/end when there is one (falling
			-- back to earliest/latest by date otherwise); first_date/latest_date
			-- stay plain MIN/MAX regardless — "first/last disclosure" is a factual
			-- date question independent of which disclosure's % we trust.
			(array_agg(pct_before ORDER BY is_chain_start DESC, COALESCE(trade_date_to, broadcast_at::date) ASC))[1],
			MIN(COALESCE(trade_date_to, broadcast_at::date)),
			(array_agg(qty_before ORDER BY is_chain_start DESC, COALESCE(trade_date_to, broadcast_at::date) ASC))[1],
			(array_agg(pct_after ORDER BY is_chain_end DESC, COALESCE(trade_date_to, broadcast_at::date) DESC))[1],
			MAX(COALESCE(trade_date_to, broadcast_at::date)),
			(array_agg(qty_after ORDER BY is_chain_end DESC, COALESCE(trade_date_to, broadcast_at::date) DESC))[1],
			COUNT(*),
			now()
		FROM chain_ends
		GROUP BY 1, symbol
		ON CONFLICT (person_key, symbol) DO UPDATE SET
			first_pct        = EXCLUDED.first_pct,
			first_date       = EXCLUDED.first_date,
			first_qty_before = EXCLUDED.first_qty_before,
			latest_pct       = EXCLUDED.latest_pct,
			latest_date      = EXCLUDED.latest_date,
			latest_qty_after = EXCLUDED.latest_qty_after,
			updated_at       = now()`)
	return err
}
