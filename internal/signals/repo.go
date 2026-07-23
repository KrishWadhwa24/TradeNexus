// Package signals is the audit store for generated scanner signals. It supports
// idempotent inserts (so re-scans/backfills don't duplicate), filtered listing,
// and retention cleanup.
package signals

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Signal is one audit record.
type Signal struct {
	ID           int64           `json:"id"`
	InstrumentID int64           `json:"instrument_id"`
	Symbol       string          `json:"symbol,omitempty"`
	Source       string          `json:"source"`
	ScannerName  string          `json:"scanner_name"`
	Timeframe    string          `json:"timeframe"`
	Direction    string          `json:"direction"`
	CandleDate   time.Time       `json:"candle_date"`
	Confidence   *int            `json:"confidence,omitempty"`
	RSI          *float64           `json:"rsi,omitempty"`
	Volume       *float64           `json:"volume,omitempty"`
	Reasons      map[string]bool    `json:"reasons"`
	Metrics      map[string]float64 `json:"metrics,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
}

// metricsJSON marshals Metrics, defaulting to an empty object (never SQL NULL).
func metricsJSON(m map[string]float64) []byte {
	if len(m) == 0 {
		return []byte("{}")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// Filter narrows a List query. Zero values mean "no filter".
type Filter struct {
	InstrumentID *int64
	Timeframe    string
	Source       string
	Limit        int
	// UserID, when set, restricts results to instruments on that user's
	// watchlists. Empty means no restriction (admin / unscoped view).
	UserID string
}

// Repo is the signals datastore.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo builds the repository.
func NewRepo(pool *pgxpool.Pool) *Repo { return &Repo{pool: pool} }

// Upsert inserts a signal, ignoring duplicates (idempotency key). It returns
// whether a new row was actually inserted.
func (r *Repo) Upsert(ctx context.Context, s Signal) (inserted bool, id int64, err error) {
	reasons, _ := json.Marshal(s.Reasons)
	err = r.pool.QueryRow(ctx, `
		INSERT INTO signals
			(instrument_id, source, scanner_name, timeframe, direction, candle_date, confidence, reasons, rsi, volume, metrics)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (instrument_id, source, scanner_name, timeframe, candle_date) DO NOTHING
		RETURNING id`,
		s.InstrumentID, s.Source, s.ScannerName, s.Timeframe, s.Direction,
		s.CandleDate, s.Confidence, reasons, s.RSI, s.Volume, metricsJSON(s.Metrics)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, nil // duplicate — already recorded
	}
	if err != nil {
		return false, 0, err
	}
	return true, id, nil
}

// List returns signals matching the filter, newest first.
func (r *Repo) List(ctx context.Context, f Filter) ([]Signal, error) {
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT s.id, s.instrument_id, i.trading_symbol, s.source, s.scanner_name, s.timeframe,
		       s.direction, s.candle_date, s.confidence, s.reasons, s.created_at, s.rsi, s.volume, s.metrics
		FROM signals s
		JOIN instruments i ON i.id = s.instrument_id
		WHERE ($1::bigint IS NULL OR s.instrument_id = $1)
		  AND ($2 = '' OR s.timeframe = $2)
		  AND ($3 = '' OR s.source = $3)
		  AND ($5 = '' OR EXISTS (
		        SELECT 1 FROM watchlists w
		        JOIN watchlist_items wi ON wi.watchlist_id = w.id
		        WHERE w.user_id = $5::uuid AND wi.instrument_id = s.instrument_id
		      ))
		ORDER BY s.created_at DESC
		LIMIT $4`,
		f.InstrumentID, f.Timeframe, f.Source, limit, f.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Signal
	for rows.Next() {
		var s Signal
		var reasons []byte
		var metrics []byte
		var conf sql.NullInt64
		var rsi sql.NullFloat64
		var volume sql.NullFloat64
		if err := rows.Scan(&s.ID, &s.InstrumentID, &s.Symbol, &s.Source, &s.ScannerName,
			&s.Timeframe, &s.Direction, &s.CandleDate, &conf, &reasons, &s.CreatedAt, &rsi, &volume, &metrics); err != nil {
			return nil, err
		}
		if conf.Valid {
			c := int(conf.Int64)
			s.Confidence = &c
		}
		if rsi.Valid {
			s.RSI = &rsi.Float64
		}
		if volume.Valid {
			s.Volume = &volume.Float64
		}
		_ = json.Unmarshal(reasons, &s.Reasons)
		_ = json.Unmarshal(metrics, &s.Metrics)
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetByID fetches a single signal.
func (r *Repo) GetByID(ctx context.Context, id int64) (Signal, error) {
	var s Signal
	var reasons []byte
	var metrics []byte
	var conf sql.NullInt64
	var rsi sql.NullFloat64
	var volume sql.NullFloat64
	err := r.pool.QueryRow(ctx, `
		SELECT s.id, s.instrument_id, i.trading_symbol, s.source, s.scanner_name, s.timeframe,
		       s.direction, s.candle_date, s.confidence, s.reasons, s.created_at, s.rsi, s.volume, s.metrics
		FROM signals s JOIN instruments i ON i.id = s.instrument_id
		WHERE s.id = $1`, id).
		Scan(&s.ID, &s.InstrumentID, &s.Symbol, &s.Source, &s.ScannerName, &s.Timeframe,
			&s.Direction, &s.CandleDate, &conf, &reasons, &s.CreatedAt, &rsi, &volume, &metrics)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, errors.New("signal not found")
	}
	if err != nil {
		return s, err
	}
	if conf.Valid {
		c := int(conf.Int64)
		s.Confidence = &c
	}
	if rsi.Valid {
		s.RSI = &rsi.Float64
	}
	if volume.Valid {
		s.Volume = &volume.Float64
	}
	_ = json.Unmarshal(reasons, &s.Reasons)
	_ = json.Unmarshal(metrics, &s.Metrics)
	return s, nil
}

// DeleteOlderThan removes signals created before now-age. Returns rows deleted.
func (r *Repo) DeleteOlderThan(ctx context.Context, age time.Duration) (int64, error) {
	cutoff := time.Now().Add(-age)
	tag, err := r.pool.Exec(ctx, `DELETE FROM signals WHERE created_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
