package optionsalgo

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"tradenexus/internal/market"
	"tradenexus/internal/store"
)

// testRepo connects to a real local Postgres — same pattern as
// fiidii.testRepo / investors' repo_test.go — and skips (not fails) if one
// isn't reachable.
func testRepo(t *testing.T) *Repo {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://tradenexus:tradenexus@localhost:5432/tradenexus?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := store.RunMigrations(dsn); err != nil {
		t.Skipf("migrations unavailable: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("connect postgres: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("ping postgres: %v", err)
	}
	t.Cleanup(pool.Close)
	return NewRepo(pool)
}

// testInstrumentID picks a real row from `instruments` to satisfy
// minute_candles' foreign key — this package doesn't own instrument
// creation, so it borrows whatever's already there rather than inserting a
// throwaway row into a table it doesn't manage.
func testInstrumentID(t *testing.T, r *Repo) int64 {
	t.Helper()
	var id int64
	if err := r.pool.QueryRow(context.Background(), `SELECT id FROM instruments LIMIT 1`).Scan(&id); err != nil {
		t.Skipf("no instrument rows available to test against: %v", err)
	}
	return id
}

func TestUpsertAndGetMinuteCandles(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	instID := testInstrumentID(t, r)
	t.Cleanup(func() { r.pool.Exec(ctx, `DELETE FROM minute_candles WHERE instrument_id=$1`, instID) })

	base := time.Date(2026, 9, 4, 9, 15, 0, 0, market.IST)
	bars := []market.Candle{
		{Time: base, Open: 100, High: 101, Low: 99, Close: 100.5, Volume: 1000},
		{Time: base.Add(1 * time.Minute), Open: 100.5, High: 102, Low: 100, Close: 101.5, Volume: 1200},
		{Time: base.Add(2 * time.Minute), Open: 101.5, High: 103, Low: 101, Close: 102.5, Volume: 900},
	}

	n, err := r.UpsertMinuteCandles(ctx, instID, bars)
	if err != nil {
		t.Fatalf("UpsertMinuteCandles: %v", err)
	}
	if n != 3 {
		t.Fatalf("upserted %d, want 3", n)
	}

	got, err := r.GetMinuteCandles(ctx, instID, 10)
	if err != nil {
		t.Fatalf("GetMinuteCandles: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d candles, want 3", len(got))
	}
	// Oldest-first: the first returned bar must be the 09:15 one, not the last inserted.
	if !got[0].Time.Equal(base) {
		t.Errorf("got[0].Time = %v, want %v (oldest-first ordering)", got[0].Time, base)
	}
	if !got[2].Time.Equal(base.Add(2 * time.Minute)) {
		t.Errorf("got[2].Time = %v, want the most recent bar last", got[2].Time)
	}

	// Re-upsert the first bar with a different close — must overwrite, not duplicate.
	updated := bars[0]
	updated.Close = 999
	if _, err := r.UpsertMinuteCandles(ctx, instID, []market.Candle{updated}); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got, err = r.GetMinuteCandles(ctx, instID, 10)
	if err != nil {
		t.Fatalf("GetMinuteCandles after re-upsert: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("re-upsert created a duplicate row: got %d candles, want 3", len(got))
	}
	if got[0].Close != 999 {
		t.Errorf("re-upsert didn't overwrite: got[0].Close = %v, want 999", got[0].Close)
	}

	latest, err := r.LatestCandleTime(ctx, instID)
	if err != nil {
		t.Fatalf("LatestCandleTime: %v", err)
	}
	if !latest.Equal(base.Add(2 * time.Minute)) {
		t.Errorf("LatestCandleTime = %v, want %v", latest, base.Add(2*time.Minute))
	}
}

func TestLatestCandleTime_NoRows(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	instID := testInstrumentID(t, r)
	t.Cleanup(func() { r.pool.Exec(ctx, `DELETE FROM minute_candles WHERE instrument_id=$1`, instID) })

	latest, err := r.LatestCandleTime(ctx, instID)
	if err != nil {
		t.Fatalf("LatestCandleTime: %v", err)
	}
	if !latest.IsZero() && latest.Year() > 1980 {
		t.Errorf("expected an epoch/zero-ish time with no rows, got %v", latest)
	}
}

func TestGetConfig_SeededDefaultsMatchScript(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()

	cfg, err := r.GetConfig(ctx)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if cfg.RiskPerTradePercent != 1.0 || cfg.DeltaTarget != 0.60 || cfg.MaxTradesPerDay != 1 {
		t.Errorf("seeded defaults don't match the script: %+v", cfg)
	}
}

func TestUpdateConfig_RoundTrips(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	original, err := r.GetConfig(ctx)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	t.Cleanup(func() { r.UpdateConfig(ctx, original) })

	updated := original
	updated.RiskPerTradePercent = 3.0
	updated.DeltaTarget = 0.65
	if err := r.UpdateConfig(ctx, updated); err != nil {
		t.Fatalf("UpdateConfig: %v", err)
	}

	got, err := r.GetConfig(ctx)
	if err != nil {
		t.Fatalf("GetConfig after update: %v", err)
	}
	if got.RiskPerTradePercent != 3.0 || got.DeltaTarget != 0.65 {
		t.Errorf("update didn't round-trip: %+v", got)
	}
}
