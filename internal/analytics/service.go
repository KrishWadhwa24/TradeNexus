// Package analytics aggregates the signal audit data for the dashboard and
// powers the Excel export.
package analytics

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Filter narrows analytics queries. Nil/empty fields mean "no filter".
type Filter struct {
	From      *time.Time
	To        *time.Time
	Timeframe string
	Source    string
}

// Stats is the aggregated dashboard summary.
type Stats struct {
	Total          int            `json:"total"`
	ByTimeframe    map[string]int `json:"by_timeframe"`
	BySource       map[string]int `json:"by_source"`
	ByDirection    map[string]int `json:"by_direction"`
	ByScanner      map[string]int `json:"by_scanner"`
	ConfidenceDist map[string]int `json:"confidence_distribution"`
}

// Row is one flattened signal for the Excel export.
type Row struct {
	ID          int64
	InstrumentID int64
	Symbol      string
	Source      string
	Scanner     string
	Timeframe   string
	Direction   string
	CandleDate  time.Time
	Confidence  *int
	CreatedAt   time.Time
}

// Service provides analytics queries.
type Service struct{ pool *pgxpool.Pool }

// NewService builds the analytics service.
func NewService(pool *pgxpool.Pool) *Service { return &Service{pool: pool} }

// where builds the shared WHERE clause + args from a filter.
func (s *Service) where(f Filter) (string, []any) {
	clauses := "WHERE 1=1"
	var args []any
	i := 1
	if f.From != nil {
		clauses += fmt.Sprintf(" AND created_at >= $%d", i)
		args = append(args, *f.From)
		i++
	}
	if f.To != nil {
		clauses += fmt.Sprintf(" AND created_at <= $%d", i)
		args = append(args, *f.To)
		i++
	}
	if f.Timeframe != "" {
		clauses += fmt.Sprintf(" AND timeframe = $%d", i)
		args = append(args, f.Timeframe)
		i++
	}
	if f.Source != "" {
		clauses += fmt.Sprintf(" AND source = $%d", i)
		args = append(args, f.Source)
		i++
	}
	return clauses, args
}

// Summary computes the aggregated dashboard stats.
func (s *Service) Summary(ctx context.Context, f Filter) (Stats, error) {
	where, args := s.where(f)
	st := Stats{
		ByTimeframe:    map[string]int{},
		BySource:       map[string]int{},
		ByDirection:    map[string]int{},
		ByScanner:      map[string]int{},
		ConfidenceDist: map[string]int{},
	}

	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM signals "+where, args...).Scan(&st.Total); err != nil {
		return st, err
	}
	for _, gc := range []struct {
		col string
		dst map[string]int
	}{
		{"timeframe", st.ByTimeframe},
		{"source", st.BySource},
		{"direction", st.ByDirection},
		{"scanner_name", st.ByScanner},
	} {
		if err := s.groupCount(ctx, gc.col, where, args, gc.dst); err != nil {
			return st, err
		}
	}

	// Confidence distribution (weekly only; NULL for pine).
	rows, err := s.pool.Query(ctx,
		"SELECT confidence, count(*) FROM signals "+where+" AND confidence IS NOT NULL GROUP BY confidence", args...)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var c, n int
		if err := rows.Scan(&c, &n); err != nil {
			return st, err
		}
		st.ConfidenceDist[strconv.Itoa(c)] = n
	}
	return st, rows.Err()
}

func (s *Service) groupCount(ctx context.Context, col, where string, args []any, dst map[string]int) error {
	// col is from a fixed internal set — safe to interpolate.
	rows, err := s.pool.Query(ctx, "SELECT "+col+", count(*) FROM signals "+where+" GROUP BY "+col, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return err
		}
		dst[k] = n
	}
	return rows.Err()
}

// Rows returns flattened signals (joined with symbol) for export, newest first.
func (s *Service) Rows(ctx context.Context, f Filter) ([]Row, error) {
	where, args := s.where(f)
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.instrument_id, i.trading_symbol, s.source, s.scanner_name,
		       s.timeframe, s.direction, s.candle_date, s.confidence, s.created_at
		FROM signals s
		JOIN instruments i ON i.id = s.instrument_id
		`+where+`
		ORDER BY s.created_at DESC
		LIMIT 5000`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Row
	for rows.Next() {
		var r Row
		var conf *int
		if err := rows.Scan(&r.ID, &r.InstrumentID, &r.Symbol, &r.Source, &r.Scanner,
			&r.Timeframe, &r.Direction, &r.CandleDate, &conf, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.Confidence = conf
		out = append(out, r)
	}
	return out, rows.Err()
}
