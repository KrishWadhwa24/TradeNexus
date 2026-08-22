package promoter

import "context"

// accumulatePosition folds one newly-inserted trade into that person's
// permanent (person_key, symbol) position. Only ever called from InsertTrade
// for a trade that was just actually inserted, so each disclosure is folded
// in exactly once, ever — the position keeps growing/shrinking even after
// promoter_trades' own retention prune deletes the row that fed it.
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
	_, err := r.pool.Exec(ctx, `
		INSERT INTO promoter_positions
			(person_key, symbol, person_name, company_name, category,
			 buy_qty, sell_qty, buy_value, sell_value,
			 first_pct, first_date, latest_pct, latest_date, disclosure_count, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$11,1,now())
		ON CONFLICT (person_key, symbol) DO UPDATE SET
			person_name   = EXCLUDED.person_name,
			company_name  = EXCLUDED.company_name,
			buy_qty       = promoter_positions.buy_qty + EXCLUDED.buy_qty,
			sell_qty      = promoter_positions.sell_qty + EXCLUDED.sell_qty,
			buy_value     = promoter_positions.buy_value + EXCLUDED.buy_value,
			sell_value    = promoter_positions.sell_value + EXCLUDED.sell_value,
			first_pct     = CASE WHEN $11 <= promoter_positions.first_date THEN $10 ELSE promoter_positions.first_pct END,
			first_date    = LEAST(promoter_positions.first_date, $11),
			category      = CASE WHEN $11 >= promoter_positions.latest_date THEN EXCLUDED.category ELSE promoter_positions.category END,
			latest_pct    = CASE WHEN $11 >= promoter_positions.latest_date THEN $12 ELSE promoter_positions.latest_pct END,
			latest_date   = GREATEST(promoter_positions.latest_date, $11),
			disclosure_count = promoter_positions.disclosure_count + 1,
			updated_at    = now()`,
		key, t.Symbol, t.PersonName, t.CompanyName, t.Category,
		buyQty, sellQty, buyValue, sellValue,
		t.PctBefore, date, t.PctAfter)
	return err
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

// BackfillPositions seeds promoter_positions from whatever's currently in
// promoter_trades, for (person, symbol) pairs that don't already have a
// position row. ON CONFLICT DO NOTHING (not DO UPDATE) deliberately — safe
// to run more than once or after accumulation has already started: it can
// only ever fill in gaps, never overwrite/clobber history that's since grown
// beyond what promoter_trades currently holds.
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
		)
		INSERT INTO promoter_positions
			(person_key, symbol, person_name, company_name, category,
			 buy_qty, sell_qty, buy_value, sell_value,
			 first_pct, first_date, latest_pct, latest_date, disclosure_count, updated_at)
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
			(array_agg(pct_before ORDER BY COALESCE(trade_date_to, broadcast_at::date) ASC))[1],
			MIN(COALESCE(trade_date_to, broadcast_at::date)),
			(array_agg(pct_after ORDER BY COALESCE(trade_date_to, broadcast_at::date) DESC))[1],
			MAX(COALESCE(trade_date_to, broadcast_at::date)),
			COUNT(*),
			now()
		FROM dedup
		GROUP BY 1, symbol
		ON CONFLICT (person_key, symbol) DO NOTHING`)
	return err
}
