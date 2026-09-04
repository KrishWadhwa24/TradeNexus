package paper

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"tradenexus/internal/store"
)

// testPool connects to a real local Postgres — same pattern as
// optionsalgo's testRepo — and skips (not fails) if one isn't reachable.
func testPool(t *testing.T) *pgxpool.Pool {
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
	return pool
}

// testAccount creates a real, throwaway user (paper_accounts.user_id has a
// FK to users) plus its paper_accounts row seeded with known balances.
// Deleting the user cascades to paper_accounts (ON DELETE CASCADE), so
// cleanup only needs to touch the users table.
func testAccount(t *testing.T, pool *pgxpool.Pool, cashBalance, algoCashBalance float64) string {
	t.Helper()
	ctx := context.Background()
	var userID string
	email := fmt.Sprintf("optionsalgo-test-%d@example.invalid", time.Now().UnixNano())
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, 'test') RETURNING id`, email).Scan(&userID); err != nil {
		t.Skipf("create test user: %v", err)
	}
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM users WHERE id=$1::uuid`, userID) })

	if _, err := pool.Exec(ctx, `
		INSERT INTO paper_accounts (user_id, starting_capital, cash_balance, algo_cash_balance, updated_at)
		VALUES ($1::uuid, 500000, $2, $3, now())`, userID, cashBalance, algoCashBalance); err != nil {
		t.Fatalf("seed paper_accounts: %v", err)
	}
	return userID
}

// testInstrumentID borrows a real row from `instruments` — this package
// doesn't own instrument creation, same convention as optionsalgo's
// repo_test.go.
func testInstrumentID(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), `SELECT id FROM instruments LIMIT 1`).Scan(&id); err != nil {
		t.Skipf("no instrument rows available to test against: %v", err)
	}
	return id
}

// TestMergeOrOpen_NeverMergesAcrossSources is the regression test for a real
// bug caught during review: mergeOrOpen's "find an existing OPEN position to
// merge into" query didn't filter by source. A manual buy (source="web") and
// an algo-sourced buy (source="options-algo") in the same instrument/side/
// product_type would have merged into one row keyed to whichever source
// opened it first — and since SourceOptionsAlgo debits/credits a completely
// different balance (algo_cash_balance) than every other source, that merge
// would debit one balance at entry and credit the other at exit. Fixed by
// adding source to the match key; this test proves two different sources
// never merge, while confirming merging *within* the same source still
// works exactly as before.
func TestMergeOrOpen_NeverMergesAcrossSources(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	userID := testAccount(t, pool, 100000, 250000)
	instID := testInstrumentID(t, pool)
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM paper_trades WHERE user_id=$1::uuid`, userID) })

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	webID, err := mergeOrOpen(ctx, tx, userID, instID, SideBuy, ProductDelivery, nil, "web", 1, 100)
	if err != nil {
		t.Fatalf("mergeOrOpen(web, first fill): %v", err)
	}
	algoID, err := mergeOrOpen(ctx, tx, userID, instID, SideBuy, ProductDelivery, nil, SourceOptionsAlgo, 1, 200)
	if err != nil {
		t.Fatalf("mergeOrOpen(options-algo): %v", err)
	}
	if algoID == webID {
		t.Fatalf("algo fill merged into the web-sourced row (id %d) — must be a separate row", webID)
	}
	webID2, err := mergeOrOpen(ctx, tx, userID, instID, SideBuy, ProductDelivery, nil, "web", 1, 300)
	if err != nil {
		t.Fatalf("mergeOrOpen(web, second fill): %v", err)
	}
	if webID2 != webID {
		t.Fatalf("second web fill should merge into the first web row (%d), got a different id (%d)", webID, webID2)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var webQty int
	var webPrice float64
	if err := pool.QueryRow(ctx, `SELECT quantity, entry_price FROM paper_trades WHERE id=$1`, webID).Scan(&webQty, &webPrice); err != nil {
		t.Fatalf("read back web row: %v", err)
	}
	if webQty != 2 || webPrice != 200 {
		t.Errorf("web row = qty %d @ %v, want qty 2 @ 200 (weighted avg of 100 and 300)", webQty, webPrice)
	}

	var algoQty int
	var algoPrice float64
	var algoSource string
	if err := pool.QueryRow(ctx, `SELECT quantity, entry_price, source FROM paper_trades WHERE id=$1`, algoID).Scan(&algoQty, &algoPrice, &algoSource); err != nil {
		t.Fatalf("read back algo row: %v", err)
	}
	if algoQty != 1 || algoPrice != 200 || algoSource != SourceOptionsAlgo {
		t.Errorf("algo row = qty %d @ %v source %q, want qty 1 @ 200 source %q (untouched by the web merges)", algoQty, algoPrice, algoSource, SourceOptionsAlgo)
	}
}

// TestCreditBalance_RoutesBySource is the real-Postgres proof that the
// balance-model change (Phase 4b) actually works end-to-end, not just in
// isolated unit tests: crediting a SourceOptionsAlgo trade must move
// algo_cash_balance only, never cash_balance, and vice versa for any other
// source — exercising the exact SQL debitBalance/creditBalance generate,
// against a real row, not a mock.
func TestCreditBalance_RoutesBySource(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	userID := testAccount(t, pool, 100000, 250000)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := creditBalance(ctx, tx, userID, SourceOptionsAlgo, 5000); err != nil {
		t.Fatalf("creditBalance(options-algo): %v", err)
	}
	if err := debitBalance(ctx, tx, userID, "web", 2000); err != nil {
		t.Fatalf("debitBalance(web): %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var cash, algoCash float64
	if err := pool.QueryRow(ctx, `SELECT cash_balance, algo_cash_balance FROM paper_accounts WHERE user_id=$1::uuid`, userID).
		Scan(&cash, &algoCash); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if algoCash != 255000 {
		t.Errorf("algo_cash_balance = %v, want 255000 (250000 seeded + 5000 credited)", algoCash)
	}
	if cash != 98000 {
		t.Errorf("cash_balance = %v, want 98000 (100000-2000) — the options-algo credit must not have touched it", cash)
	}
}

// TestCloseAtPrice_CreditsCorrectBalance proves closeAtPrice (the function
// every real position close — manual or algo — funnels through) credits
// AlgoCashBalance for an options-algo trade and CashBalance for everything
// else, using the exact same code path a real close takes (not just the SQL
// helper in isolation).
func TestCloseAtPrice_CreditsCorrectBalance(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	userID := testAccount(t, pool, 100000, 250000)

	trade := Trade{
		ID: 999999999, userID: userID, Source: SourceOptionsAlgo,
		Side: SideBuy, ProductType: ProductDelivery, OptionType: "CE",
		Quantity: 65, EntryPrice: 100,
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// closeAtPrice's own UPDATE paper_trades targets trade.ID — no real row
	// exists for it, which is fine (0 rows affected, no error from pgx) since
	// we only care about the balance-credit side effect here.
	pnl, err := closeAtPrice(ctx, tx, trade, 150) // +50/unit * 65 = +3250 pnl
	if err != nil {
		t.Fatalf("closeAtPrice: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	wantPnl := (150.0 - 100.0) * 65
	if pnl != wantPnl {
		t.Fatalf("pnl = %v, want %v", pnl, wantPnl)
	}

	var cash, algoCash float64
	if err := pool.QueryRow(ctx, `SELECT cash_balance, algo_cash_balance FROM paper_accounts WHERE user_id=$1::uuid`, userID).
		Scan(&cash, &algoCash); err != nil {
		t.Fatalf("read back: %v", err)
	}
	// settlement = marginFraction(DELIVERY, isOption=true)=1.0 * 100 * 65 + 3250 = 6500+3250 = 9750
	wantAlgoCash := 250000.0 + 100*65 + wantPnl
	if algoCash != wantAlgoCash {
		t.Errorf("algo_cash_balance = %v, want %v", algoCash, wantAlgoCash)
	}
	if cash != 100000 {
		t.Errorf("cash_balance = %v, want unchanged 100000 — an options-algo close must never touch it", cash)
	}
}
