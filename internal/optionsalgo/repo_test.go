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

// TestGetConfig_IsWellFormed checks structural sanity, not exact default
// values — algo_config is a single, live-editable row (that's the entire
// point of Phase 4a), so a prior live edit (a real admin changing risk% for
// a day, or an earlier test run) legitimately leaves it holding non-default
// values. Exact round-trip correctness is covered separately by
// TestUpdateConfig_RoundTrips, which saves and restores the original row.
func TestGetConfig_IsWellFormed(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()

	cfg, err := r.GetConfig(ctx)
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if cfg.RiskPerTradePercent <= 0 || cfg.DeltaTarget <= 0 || cfg.DeltaTarget >= 1 || cfg.MaxTradesPerDay <= 0 {
		t.Errorf("config has an implausible value: %+v", cfg)
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

func TestLogDecision_AndRecentDecisions(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()

	tradeID := int64(123456789)
	pnl, mfe, mae, exitPx := 500.0, 800.0, -100.0, 150.0
	d := Decision{
		EvaluatedAt: time.Now().In(market.IST),
		NiftySpot:   23900, ORHigh: 23950, ORLow: 23800,
		VWAP: 23920, EMAFast: 23910, EMASlow: 23890, ATR: 30, ATRAvg: 20,
		Direction: "BULLISH", DirectionReason: "test reason",
		SelectedSymbol: "NIFTY08SEP2623900CE", SelectedStrike: 23900,
		SelectedDelta: 0.6, SelectedIV: 12.5, SelectedTheta: -15,
		SelectionReason: "delta closest to target",
		EntryOK:         true, EntryReason: "all conditions met",
		Action: "EXIT", TradeID: &tradeID,
		ExitPrice: &exitPx, ExitReason: "stop hit", PnL: &pnl, MFE: &mfe, MAE: &mae,
		Detail: "test decision row",
	}
	if err := r.LogDecision(ctx, d); err != nil {
		t.Fatalf("LogDecision: %v", err)
	}
	t.Cleanup(func() { r.pool.Exec(ctx, `DELETE FROM algo_decisions WHERE trade_id=$1`, tradeID) })

	recent, err := r.RecentDecisions(ctx, 10)
	if err != nil {
		t.Fatalf("RecentDecisions: %v", err)
	}
	if len(recent) == 0 {
		t.Fatal("expected at least one decision")
	}
	got := recent[0] // newest first
	if got.Action != "EXIT" || got.TradeID == nil || *got.TradeID != tradeID {
		t.Errorf("got action=%s tradeID=%v, want EXIT/%d", got.Action, got.TradeID, tradeID)
	}
	if got.PnL == nil || *got.PnL != 500.0 {
		t.Errorf("PnL didn't round-trip: %v", got.PnL)
	}
	if got.MFE == nil || *got.MFE != 800.0 || got.MAE == nil || *got.MAE != -100.0 {
		t.Errorf("MFE/MAE didn't round-trip: mfe=%v mae=%v", got.MFE, got.MAE)
	}
	if got.SelectedDelta != 0.6 || got.Direction != "BULLISH" {
		t.Errorf("context fields didn't round-trip: %+v", got)
	}
}

// TestOptionExpiryAtLeastDaysOut_SkipsNearExpiries is the regression test
// for A1: buying the nearest expiry maximizes theta decay for a long
// option buyer. Checked against whatever NIFTY expiries are actually synced
// right now (not a fixture) — same discipline as verifying this live before
// building it.
func TestOptionExpiryAtLeastDaysOut_SkipsNearExpiries(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()

	nearest, err := r.NearestOptionExpiry(ctx, "NIFTY")
	if err != nil {
		t.Skipf("no NIFTY expiries synced: %v", err)
	}

	// minDays=0 must behave exactly like "nearest" — same underlying query,
	// just phrased as a floor of zero days out.
	same, err := r.OptionExpiryAtLeastDaysOut(ctx, "NIFTY", 0)
	if err != nil {
		t.Fatalf("OptionExpiryAtLeastDaysOut(0): %v", err)
	}
	if !same.Equal(nearest) {
		t.Errorf("OptionExpiryAtLeastDaysOut(0) = %v, want it to match NearestOptionExpiry %v", same, nearest)
	}

	// A real minDays must actually skip the near dates, not just re-derive
	// the nearest one.
	farOut, err := r.OptionExpiryAtLeastDaysOut(ctx, "NIFTY", 21)
	if err != nil {
		t.Skipf("no NIFTY expiry synced 21+ days out yet: %v", err)
	}
	if !farOut.After(nearest) && !farOut.Equal(nearest) {
		t.Errorf("farOut expiry %v should be >= nearest %v", farOut, nearest)
	}
	if daysOut := farOut.Sub(time.Now()).Hours() / 24; daysOut < 21 {
		t.Errorf("selected expiry %v is only %.1f days out, want >= 21", farOut, daysOut)
	}

	// Nothing synced that far out must error, not silently return zero.
	if _, err := r.OptionExpiryAtLeastDaysOut(ctx, "NIFTY", 100000); err == nil {
		t.Error("expected an error when no expiry is synced that far out")
	}
}
