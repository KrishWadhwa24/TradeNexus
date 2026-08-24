package deals

import "context"

// isDuplicateFundTrade reports whether the just-inserted row (id newID)
// duplicates another already-stored row's real-world trade. Two known ways
// this happens in NSE's raw feed, neither caught by market_deals' own
// unique constraint (which keys on deal_type + the raw, un-normalized
// client_name):
//   - the identical trade disclosed as both a bulk deal and a block deal
//   - the identical trade disclosed twice within the same feed with a
//     formatting variant of the client name (e.g. a trailing period)
// Both rows are still kept in market_deals (each is a genuine disclosure,
// worth showing on its own feed/search), but only one should ever be folded
// into mutual_fund_positions, or that one real trade gets double-counted.
func (r *Repo) isDuplicateFundTrade(ctx context.Context, newID int64, row Row) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM market_deals
			WHERE id <> $1
				AND deal_date = $2
				AND symbol = $3
				AND regexp_replace(upper(btrim(client_name)), '[.\s]+$', '') = $4
				AND buy_sell = $5
				AND quantity = $6
				AND price = $7
		)`,
		newID, dateOnly(row.Date), row.Symbol, normalizeFundName(row.ClientName), row.Side, row.Quantity, row.Price,
	).Scan(&exists)
	return exists, err
}

// accumulateFundPosition adds one newly-inserted mutual-fund deal row's
// qty/value into that fund's permanent (fund_name, symbol) position. Only
// ever called from InsertRows for a row that was just actually inserted, so
// each row is folded in exactly once, ever — the position keeps growing
// even after market_deals' own retention prune deletes the row that fed it.
func (r *Repo) accumulateFundPosition(ctx context.Context, row Row) error {
	fund := normalizeFundName(row.ClientName)
	value := float64(row.Quantity) * row.Price
	var buyQty, sellQty int64
	var buyValue, sellValue float64
	if row.Side == "BUY" {
		buyQty, buyValue = row.Quantity, value
	} else {
		sellQty, sellValue = row.Quantity, value
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO mutual_fund_positions
			(fund_name, symbol, security_name, buy_qty, sell_qty, buy_value, sell_value, deal_count, first_deal_date, last_deal_date, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,1,$8,$8,now())
		ON CONFLICT (fund_name, symbol) DO UPDATE SET
			security_name   = EXCLUDED.security_name,
			buy_qty         = mutual_fund_positions.buy_qty + EXCLUDED.buy_qty,
			sell_qty        = mutual_fund_positions.sell_qty + EXCLUDED.sell_qty,
			buy_value       = mutual_fund_positions.buy_value + EXCLUDED.buy_value,
			sell_value      = mutual_fund_positions.sell_value + EXCLUDED.sell_value,
			deal_count      = mutual_fund_positions.deal_count + 1,
			first_deal_date = LEAST(mutual_fund_positions.first_deal_date, EXCLUDED.first_deal_date),
			last_deal_date  = GREATEST(mutual_fund_positions.last_deal_date, EXCLUDED.last_deal_date),
			updated_at      = now()`,
		fund, row.Symbol, row.SecurityName, buyQty, sellQty, buyValue, sellValue, dateOnly(row.Date))
	return err
}

// ListFundPositions returns one summary row per fund, largest gross value
// (buy+sell) first.
func (r *Repo) ListFundPositions(ctx context.Context) ([]FundSummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT fund_name,
			COUNT(*) AS stock_count,
			SUM(buy_value) AS buy_value,
			SUM(sell_value) AS sell_value,
			MIN(first_deal_date) AS first_deal_date,
			MAX(last_deal_date) AS last_deal_date
		FROM mutual_fund_positions
		GROUP BY fund_name
		ORDER BY SUM(buy_value) + SUM(sell_value) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FundSummary
	for rows.Next() {
		var f FundSummary
		if err := rows.Scan(&f.FundName, &f.StockCount, &f.BuyValue, &f.SellValue,
			&f.FirstDealDate, &f.LastDealDate); err != nil {
			return nil, err
		}
		f.NetValue = f.BuyValue - f.SellValue
		out = append(out, f)
	}
	return out, rows.Err()
}

// FundPositionStocks returns every stock a (normalized) fund has traded,
// largest net value first.
func (r *Repo) FundPositionStocks(ctx context.Context, fundName string) ([]FundStock, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT symbol, security_name, buy_qty, sell_qty, buy_value, sell_value, deal_count, last_deal_date
		FROM mutual_fund_positions
		WHERE fund_name = $1
		ORDER BY (buy_value - sell_value) DESC`, fundName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FundStock
	for rows.Next() {
		var st FundStock
		if err := rows.Scan(&st.Symbol, &st.SecurityName, &st.BuyQty, &st.SellQty,
			&st.BuyValue, &st.SellValue, &st.DealCount, &st.LastDealDate); err != nil {
			return nil, err
		}
		st.NetQty = st.BuyQty - st.SellQty
		st.NetValue = st.BuyValue - st.SellValue
		out = append(out, st)
	}
	return out, rows.Err()
}

// BackfillFundPositions seeds mutual_fund_positions from whatever's
// currently in market_deals, for (fund, symbol) pairs that don't already
// have a position row. ON CONFLICT DO NOTHING (not DO UPDATE) deliberately
// — this makes it safe to run more than once or after accumulation has
// already started: it can only ever fill in gaps, never overwrite/clobber
// history that's since grown beyond what market_deals currently holds.
func (r *Repo) BackfillFundPositions(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, `
		WITH distinct_deals AS (
			-- NSE sometimes discloses the identical real-world trade as both a
			-- bulk deal and a block deal (same symbol/client/side/qty/price/date).
			-- Collapse each such pair to one row before aggregating, same as the
			-- ingestion-time dedup in isDuplicateAcrossDealTypes.
			SELECT DISTINCT ON (
				regexp_replace(upper(btrim(client_name)), '[.\s]+$', ''),
				symbol, buy_sell, quantity, price, deal_date
			)
				regexp_replace(upper(btrim(client_name)), '[.\s]+$', '') AS fund_name,
				symbol, security_name, buy_sell, quantity, price, deal_date
			FROM market_deals
			WHERE client_name ILIKE '%MUTUAL FUND%'
			ORDER BY
				regexp_replace(upper(btrim(client_name)), '[.\s]+$', ''),
				symbol, buy_sell, quantity, price, deal_date, deal_type
		)
		INSERT INTO mutual_fund_positions
			(fund_name, symbol, security_name, buy_qty, sell_qty, buy_value, sell_value, deal_count, first_deal_date, last_deal_date, updated_at)
		SELECT
			fund_name,
			symbol,
			MAX(security_name),
			COALESCE(SUM(quantity) FILTER (WHERE buy_sell = 'BUY'), 0),
			COALESCE(SUM(quantity) FILTER (WHERE buy_sell = 'SELL'), 0),
			COALESCE(SUM(quantity * price) FILTER (WHERE buy_sell = 'BUY'), 0),
			COALESCE(SUM(quantity * price) FILTER (WHERE buy_sell = 'SELL'), 0),
			COUNT(*),
			MIN(deal_date),
			MAX(deal_date),
			now()
		FROM distinct_deals
		GROUP BY fund_name, symbol
		ON CONFLICT (fund_name, symbol) DO NOTHING`)
	return err
}
