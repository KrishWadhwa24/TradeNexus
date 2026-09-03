package investors

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"tradenexus/internal/store"
)

// testRepo connects to a real local Postgres — same pattern as
// fiidii.testRepo / users' repo_test.go — and skips (not fails) if one isn't
// reachable, since this exercises real SQL (upsert conflict targets, array
// binding) that a mock can't meaningfully stand in for.
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

// seedHolding inserts a position directly (bypassing UpsertHolding's own
// report_date-gating) so tests can set up an exact starting state.
func seedHolding(t *testing.T, r *Repo, investorKey, symbol string, reportDate time.Time) {
	t.Helper()
	ctx := context.Background()
	_, err := r.pool.Exec(context.Background(), `
		INSERT INTO investor_positions
			(investor_key, symbol, investor_name, company_name, shares, pct_holding, report_date, first_seen_date, updated_at)
		VALUES ($1,$2,$1,'Test Co',1000,2.5,$3,$3,now())
		ON CONFLICT (investor_key, symbol) DO UPDATE SET report_date = EXCLUDED.report_date`,
		investorKey, symbol, reportDate)
	if err != nil {
		t.Fatalf("seedHolding: %v", err)
	}
	t.Cleanup(func() {
		r.pool.Exec(ctx, `DELETE FROM investor_positions WHERE investor_key=$1 AND symbol=$2`, investorKey, symbol)
	})
}

func holdingExists(t *testing.T, r *Repo, investorKey, symbol string) bool {
	t.Helper()
	var n int
	if err := r.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM investor_positions WHERE investor_key=$1 AND symbol=$2`, investorKey, symbol,
	).Scan(&n); err != nil {
		t.Fatalf("holdingExists query: %v", err)
	}
	return n > 0
}

func TestUpsertHolding_NeverRegressesToOlderFiling(t *testing.T) {
	r := testRepo(t)
	ctx := context.Background()
	symbol := "TESTSTALE1"
	newer := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)
	older := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)

	if err := r.UpsertHolding(ctx, "TESTINV", Holding{InvestorName: "Test Inv", Symbol: symbol, PctHolding: 5, ReportDate: newer}); err != nil {
		t.Fatalf("upsert newer: %v", err)
	}
	t.Cleanup(func() { r.pool.Exec(ctx, `DELETE FROM investor_positions WHERE investor_key='TESTINV' AND symbol=$1`, symbol) })

	// A stale re-processed filing (older report_date, different pct) must not
	// overwrite the already-stored newer snapshot.
	if err := r.UpsertHolding(ctx, "TESTINV", Holding{InvestorName: "Test Inv", Symbol: symbol, PctHolding: 99, ReportDate: older}); err != nil {
		t.Fatalf("upsert older: %v", err)
	}
	holdings, err := r.HoldingsForInvestor(ctx, "TESTINV")
	if err != nil {
		t.Fatalf("HoldingsForInvestor: %v", err)
	}
	for _, h := range holdings {
		if h.Symbol == symbol && h.PctHolding != 5 {
			t.Errorf("older filing regressed pct_holding to %v, want unchanged 5", h.PctHolding)
		}
	}
}

func TestUpsertHolding_DecreaseIsApplied(t *testing.T) {
	// A stake going DOWN, while the investor is still named in the newer
	// filing, must overwrite exactly like an increase would — the update is
	// gated on report_date recency only, never on direction of change.
	r := testRepo(t)
	ctx := context.Background()
	symbol := "TESTSTALE2"
	q1 := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	q2 := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)

	if err := r.UpsertHolding(ctx, "TESTINV2", Holding{InvestorName: "Test Inv 2", Symbol: symbol, PctHolding: 18, Shares: 100000, ReportDate: q1}); err != nil {
		t.Fatalf("upsert q1: %v", err)
	}
	t.Cleanup(func() { r.pool.Exec(ctx, `DELETE FROM investor_positions WHERE investor_key='TESTINV2' AND symbol=$1`, symbol) })

	if err := r.UpsertHolding(ctx, "TESTINV2", Holding{InvestorName: "Test Inv 2", Symbol: symbol, PctHolding: 11, Shares: 60000, ReportDate: q2}); err != nil {
		t.Fatalf("upsert q2 (decrease): %v", err)
	}
	holdings, err := r.HoldingsForInvestor(ctx, "TESTINV2")
	if err != nil {
		t.Fatalf("HoldingsForInvestor: %v", err)
	}
	if len(holdings) != 1 || holdings[0].PctHolding != 11 || holdings[0].Shares != 60000 {
		t.Fatalf("decrease not applied, got %+v", holdings)
	}
}

func TestRemoveStaleHoldings_DropsInvestorNotInNewerFiling(t *testing.T) {
	r := testRepo(t)
	symbol := "TESTSTALE3"
	q1 := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	q2 := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)

	seedHolding(t, r, "STAYS", symbol, q1)
	seedHolding(t, r, "EXITS", symbol, q1)

	// q2's filing only names STAYS — EXITS has sold out / dropped below
	// threshold and must be removed.
	removed, err := r.RemoveStaleHoldings(context.Background(), symbol, q2, []string{"STAYS"})
	if err != nil {
		t.Fatalf("RemoveStaleHoldings: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if !holdingExists(t, r, "STAYS", symbol) {
		t.Error("STAYS should still be tracked")
	}
	if holdingExists(t, r, "EXITS", symbol) {
		t.Error("EXITS should have been removed")
	}
}

func TestRemoveStaleHoldings_EmptyMatchRemovesEveryone(t *testing.T) {
	r := testRepo(t)
	symbol := "TESTSTALE4"
	q1 := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	q2 := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)

	seedHolding(t, r, "GONE1", symbol, q1)
	seedHolding(t, r, "GONE2", symbol, q1)

	// Nobody tracked matched q2's filing at all — a nil slice must behave
	// exactly like an explicit empty one (delete everyone older), not like
	// SQL NULL (which would silently match nothing).
	var nilKeys []string
	removed, err := r.RemoveStaleHoldings(context.Background(), symbol, q2, nilKeys)
	if err != nil {
		t.Fatalf("RemoveStaleHoldings: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}
}

func TestRemoveStaleHoldings_OlderFilingNeverDeletesNewerRow(t *testing.T) {
	r := testRepo(t)
	symbol := "TESTSTALE5"
	q1 := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	q2 := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)

	// KEEPSNEW's stored position is already at q2 (a newer filing already
	// processed it). Reprocessing an older q1 filing that doesn't mention
	// KEEPSNEW must NOT delete it — out-of-order catch-up safety.
	seedHolding(t, r, "KEEPSNEW", symbol, q2)

	removed, err := r.RemoveStaleHoldings(context.Background(), symbol, q1, []string{})
	if err != nil {
		t.Fatalf("RemoveStaleHoldings: %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0 (row is newer than the reprocessed filing)", removed)
	}
	if !holdingExists(t, r, "KEEPSNEW", symbol) {
		t.Error("KEEPSNEW should not have been removed")
	}
}

func TestRemoveStaleHoldings_ScopedToSymbol(t *testing.T) {
	r := testRepo(t)
	symbolA := "TESTSTALE6A"
	symbolB := "TESTSTALE6B"
	q1 := time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
	q2 := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)

	// Same investor, held in two different stocks. A newer filing for stock
	// A that doesn't name them must not touch their unrelated position in B.
	seedHolding(t, r, "MULTISTOCK", symbolA, q1)
	seedHolding(t, r, "MULTISTOCK", symbolB, q1)

	removed, err := r.RemoveStaleHoldings(context.Background(), symbolA, q2, []string{})
	if err != nil {
		t.Fatalf("RemoveStaleHoldings: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if holdingExists(t, r, "MULTISTOCK", symbolA) {
		t.Error("MULTISTOCK's position in symbolA should have been removed")
	}
	if !holdingExists(t, r, "MULTISTOCK", symbolB) {
		t.Error("MULTISTOCK's unrelated position in symbolB should be untouched")
	}
}
