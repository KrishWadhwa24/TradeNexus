package calendar

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Service is a DB-backed, reloadable calendar. It caches a Calendar snapshot
// so lookups are lock-light.
type Service struct {
	pool     *pgxpool.Pool
	exchange string

	mu  sync.RWMutex
	cal *Calendar
}

// NewService builds the service with an empty (weekends-only) calendar until
// Reload is called.
func NewService(pool *pgxpool.Pool, exchange string) *Service {
	return &Service{pool: pool, exchange: exchange, cal: New(nil)}
}

// Reload loads holidays from the DB into a fresh Calendar snapshot.
func (s *Service) Reload(ctx context.Context) error {
	rows, err := s.pool.Query(ctx,
		`SELECT holiday_date FROM market_holidays WHERE exchange = $1`, s.exchange)
	if err != nil {
		return err
	}
	defer rows.Close()

	var holidays []time.Time
	for rows.Next() {
		var hd time.Time
		if err := rows.Scan(&hd); err != nil {
			return err
		}
		holidays = append(holidays, hd)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	cal := New(holidays)
	s.mu.Lock()
	s.cal = cal
	s.mu.Unlock()
	return nil
}

// Cal returns the current Calendar snapshot.
func (s *Service) Cal() *Calendar {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cal
}

// IsMarketOpen reports whether the market is open at time t.
func (s *Service) IsMarketOpen(t time.Time) bool {
	return s.Cal().IsMarketOpen(t)
}

// AddHolidays upserts holiday dates then reloads the cache.
func (s *Service) AddHolidays(ctx context.Context, dates []time.Time) (int, error) {
	batch := &pgx.Batch{}
	for _, hd := range dates {
		batch.Queue(`
			INSERT INTO market_holidays (exchange, holiday_date)
			VALUES ($1, $2) ON CONFLICT DO NOTHING`, s.exchange, hd)
	}
	br := s.pool.SendBatch(ctx, batch)
	for range dates {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return 0, err
		}
	}
	if err := br.Close(); err != nil {
		return 0, err
	}
	if err := s.Reload(ctx); err != nil {
		return 0, err
	}
	return len(dates), nil
}
