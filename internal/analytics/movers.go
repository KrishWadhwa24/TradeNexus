package analytics

import "context"

// Mover is a stock ranked by daily percentage change.
type Mover struct {
	InstrumentID int64   `json:"instrument_id"`
	Symbol       string  `json:"symbol"`
	LastClose    float64 `json:"last_close"`
	PrevClose    float64 `json:"prev_close"`
	PctChange    float64 `json:"pct_change"`
}

// TopMovers returns the tracked stocks with the highest daily % gain (last two
// daily candles). Powers the Home "trending" view.
func (s *Service) TopMovers(ctx context.Context, limit int) ([]Mover, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		WITH ranked AS (
			SELECT instrument_id, close, trade_date,
			       row_number() OVER (PARTITION BY instrument_id ORDER BY trade_date DESC) AS rn
			FROM daily_candles
		)
		SELECT r1.instrument_id, i.trading_symbol, r1.close, r2.close,
		       (r1.close - r2.close) / r2.close * 100 AS pct
		FROM ranked r1
		JOIN ranked r2 ON r2.instrument_id = r1.instrument_id AND r2.rn = 2
		JOIN instruments i ON i.id = r1.instrument_id
		WHERE r1.rn = 1 AND r2.close > 0
		ORDER BY pct DESC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Mover
	for rows.Next() {
		var m Mover
		if err := rows.Scan(&m.InstrumentID, &m.Symbol, &m.LastClose, &m.PrevClose, &m.PctChange); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
