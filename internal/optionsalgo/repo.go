// Package optionsalgo is the options-trading algo engine: 1-minute candle
// history for the underlying indices (Nifty/Sensex), and — as later steps
// land — signal generation, strike selection, and auto-execution. Everything
// here is deliberately isolated from the equity scan/reconcile/candle
// pipeline (internal/engine, internal/candles): no shared tables, no shared
// code paths, so nothing built in this package can ever change equity
// trading's behavior.
package optionsalgo

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"tradenexus/internal/market"
)

// Repo persists 1-minute OHLCV bars in minute_candles — a table separate
// from daily_candles/weekly_candles/monthly_candles (see the migration),
// used only for the underlying(s) driving the options algo.
type Repo struct{ pool *pgxpool.Pool }

// NewRepo builds the repository.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// UpsertMinuteCandles stores 1-minute bars for one instrument. Same
// batch-upsert shape as candles.Repo.UpsertDaily, keyed on the full
// timestamp instead of a trade date.
func (r *Repo) UpsertMinuteCandles(ctx context.Context, instrumentID int64, cs []market.Candle) (int, error) {
	if len(cs) == 0 {
		return 0, nil
	}
	batch := &pgx.Batch{}
	for _, c := range cs {
		batch.Queue(`
			INSERT INTO minute_candles (instrument_id, candle_time, open, high, low, close, volume)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (instrument_id, candle_time) DO UPDATE
			SET open=EXCLUDED.open, high=EXCLUDED.high, low=EXCLUDED.low,
			    close=EXCLUDED.close, volume=EXCLUDED.volume`,
			instrumentID, c.Time, c.Open, c.High, c.Low, c.Close, c.Volume)
	}
	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range cs {
		if _, err := br.Exec(); err != nil {
			return 0, fmt.Errorf("upsert minute candles: %w", err)
		}
	}
	return len(cs), nil
}

// GetMinuteCandles returns the most recent `limit` 1-minute bars for one
// instrument, oldest first (the order an indicator calculation expects).
func (r *Repo) GetMinuteCandles(ctx context.Context, instrumentID int64, limit int) ([]market.Candle, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT candle_time, open, high, low, close, volume
		FROM minute_candles
		WHERE instrument_id = $1
		ORDER BY candle_time DESC
		LIMIT $2`, instrumentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []market.Candle
	for rows.Next() {
		var c market.Candle
		if err := rows.Scan(&c.Time, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse: the query orders DESC to get the most recent `limit` rows
	// cheaply (via the primary key), but callers need oldest-first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// LatestCandleTime returns the timestamp of the most recent stored bar for
// one instrument, or the zero time if none exist yet — used by the
// live-refresh job to know how far forward it needs to fetch.
func (r *Repo) LatestCandleTime(ctx context.Context, instrumentID int64) (time.Time, error) {
	var t time.Time
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(candle_time), 'epoch'::timestamptz) FROM minute_candles WHERE instrument_id = $1`,
		instrumentID).Scan(&t)
	return t, err
}
