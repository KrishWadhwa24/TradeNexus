package candles

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"tradenexus/internal/market"
)

// Repo persists daily candles and their derived weekly/monthly aggregates.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo builds the repository.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// UpsertDaily inserts/updates daily candles for an instrument.
func (r *Repo) UpsertDaily(ctx context.Context, instrumentID int64, cs []market.Candle) (int, error) {
	if len(cs) == 0 {
		return 0, nil
	}
	batch := &pgx.Batch{}
	for _, c := range cs {
		batch.Queue(`
			INSERT INTO daily_candles (instrument_id, trade_date, open, high, low, close, volume)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
			ON CONFLICT (instrument_id, trade_date) DO UPDATE
			SET open=EXCLUDED.open, high=EXCLUDED.high, low=EXCLUDED.low,
			    close=EXCLUDED.close, volume=EXCLUDED.volume`,
			instrumentID, c.Time, c.Open, c.High, c.Low, c.Close, c.Volume)
	}
	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range cs {
		if _, err := br.Exec(); err != nil {
			return 0, fmt.Errorf("upsert daily: %w", err)
		}
	}
	return len(cs), nil
}

// GetDaily returns daily candles for an instrument ordered ascending.
func (r *Repo) GetDaily(ctx context.Context, instrumentID int64) ([]market.Candle, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT trade_date, open, high, low, close, volume
		FROM daily_candles WHERE instrument_id = $1 ORDER BY trade_date`, instrumentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []market.Candle
	for rows.Next() {
		var c market.Candle
		var d time.Time
		if err := rows.Scan(&d, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume); err != nil {
			return nil, err
		}
		c.Time = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, market.IST)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListInstrumentIDsWithData returns instrument ids that have daily candles.
// This is the "tracked universe" the scheduler scans/reconciles.
func (r *Repo) ListInstrumentIDsWithData(ctx context.Context) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT instrument_id FROM daily_candles ORDER BY instrument_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// DailyDateSet returns the set of stored trade dates (keyed "2006-01-02") plus
// the first and last stored dates. ok is false when there's no data.
func (r *Repo) DailyDateSet(ctx context.Context, instrumentID int64) (set map[string]bool, first, last time.Time, ok bool, err error) {
	rows, err := r.pool.Query(ctx,
		`SELECT trade_date FROM daily_candles WHERE instrument_id=$1 ORDER BY trade_date`, instrumentID)
	if err != nil {
		return nil, time.Time{}, time.Time{}, false, err
	}
	defer rows.Close()
	set = map[string]bool{}
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, time.Time{}, time.Time{}, false, err
		}
		d = time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, market.IST)
		if !ok {
			first = d
			ok = true
		}
		last = d
		set[d.Format("2006-01-02")] = true
	}
	return set, first, last, ok, rows.Err()
}

// RebuildAggregates recomputes and upserts weekly + monthly candles for an
// instrument from its stored daily candles. Called after any daily upsert.
func (r *Repo) RebuildAggregates(ctx context.Context, instrumentID int64) (weekly, monthly int, err error) {
	daily, err := r.GetDaily(ctx, instrumentID)
	if err != nil {
		return 0, 0, err
	}
	w := Weekly(daily)
	m := Monthly(daily)

	if err = r.upsertAgg(ctx, "weekly_candles", instrumentID, w); err != nil {
		return 0, 0, err
	}
	if err = r.upsertAgg(ctx, "monthly_candles", instrumentID, m); err != nil {
		return 0, 0, err
	}
	return len(w), len(m), nil
}

// upsertAgg atomically inserts/updates aggregate candles for an instrument.
func (r *Repo) upsertAgg(ctx context.Context, table string, instrumentID int64, cs []market.AggCandle) error {
	if len(cs) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	query := fmt.Sprintf(`
		INSERT INTO %s (instrument_id, period_start, period_end, open, high, low, close, volume, is_confirmed)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (instrument_id, period_start) DO UPDATE
		SET period_end = EXCLUDED.period_end,
			open = EXCLUDED.open,
			high = EXCLUDED.high,
			low = EXCLUDED.low,
			close = EXCLUDED.close,
			volume = EXCLUDED.volume,
			is_confirmed = EXCLUDED.is_confirmed`, table)

	for _, c := range cs {
		batch.Queue(query,
			instrumentID, c.PeriodStart, c.PeriodEnd, c.Open, c.High, c.Low, c.Close, c.Volume, c.IsConfirmed)
	}

	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < len(cs); i++ {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("upsert agg %s: %w", table, err)
		}
	}
	return nil
}

// GetAggregates returns stored weekly or monthly candles for an instrument.
// tf must be market.TF1W or market.TF1M.
func (r *Repo) GetAggregates(ctx context.Context, instrumentID int64, tf string) ([]market.AggCandle, error) {
	var table string
	switch tf {
	case market.TF1W:
		table = "weekly_candles"
	case market.TF1M:
		table = "monthly_candles"
	default:
		return nil, fmt.Errorf("invalid aggregate timeframe %q", tf)
	}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
		SELECT period_start, period_end, open, high, low, close, volume, is_confirmed
		FROM %s WHERE instrument_id=$1 ORDER BY period_start`, table), instrumentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []market.AggCandle
	for rows.Next() {
		var c market.AggCandle
		var ps, pe time.Time
		if err := rows.Scan(&ps, &pe, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume, &c.IsConfirmed); err != nil {
			return nil, err
		}
		c.PeriodStart = time.Date(ps.Year(), ps.Month(), ps.Day(), 0, 0, 0, 0, market.IST)
		c.PeriodEnd = time.Date(pe.Year(), pe.Month(), pe.Day(), 0, 0, 0, 0, market.IST)
		out = append(out, c)
	}
	return out, rows.Err()
}
