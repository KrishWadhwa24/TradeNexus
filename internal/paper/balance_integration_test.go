package paper

import (
	"context"
	"fmt"
	"math"
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
	// settlement = marginFraction(DELIVERY, isOption=true)=1.0 * 100 * 65 + 3250 = 6500+3250 = 9750,
	// MINUS the real exit charges on that sell (STT dominates — see charges.go).
	exitCharges := OptionCharges(SideSell, 150, 65)
	if exitCharges.Total <= 0 {
		t.Fatal("expected the closing sell to incur real charges")
	}
	wantAlgoCash := 250000.0 + 100*65 + wantPnl - exitCharges.Total
	if math.Abs(algoCash-wantAlgoCash) > 0.0001 {
		t.Errorf("algo_cash_balance = %v, want %v (settlement 9750 less exit charges %.4f)", algoCash, wantAlgoCash, exitCharges.Total)
	}
	if cash != 100000 {
		t.Errorf("cash_balance = %v, want unchanged 100000 — an options-algo close must never touch it", cash)
	}
}

// TestSetCapital_ExcludesAlgoPositionsFromOpenCost is the regression test for
// a real money-correctness bug caught during review: SetCapital's openCost
// query summed EVERY open position's notional cost, including options-algo
// ones — even though algo positions are funded from algo_cash_balance, not
// cash_balance. An open algo position used to silently reduce a user's
// regular cash_balance below the capital they actually asked for.
func TestSetCapital_ExcludesAlgoPositionsFromOpenCost(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	userID := testAccount(t, pool, 0, 250000)
	instID := testInstrumentID(t, pool)
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM paper_trades WHERE user_id=$1::uuid`, userID) })

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := mergeOrOpen(ctx, tx, userID, instID, SideBuy, ProductDelivery, nil, SourceOptionsAlgo, 65, 1000); err != nil {
		t.Fatalf("mergeOrOpen(options-algo): %v", err)
	}
	if _, err := mergeOrOpen(ctx, tx, userID, instID, SideBuy, ProductDelivery, nil, "web", 1, 5000); err != nil {
		t.Fatalf("mergeOrOpen(web): %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	s := &Service{pool: pool}
	acct, err := s.SetCapital(ctx, userID, 100000)
	if err != nil {
		t.Fatalf("SetCapital: %v", err)
	}
	// openCost must only see the web position (5000): cash = 100000 - 5000 =
	// 95000. The options-algo position's 65*1000=65000 notional must NOT be
	// subtracted here — it was never funded from cash_balance.
	if acct.CashBalance != 95000 {
		t.Errorf("cash_balance = %v, want 95000 (100000 - the web position's 5000 only, excluding the algo position's 65000)", acct.CashBalance)
	}
}

// TestClosePartialAtPrice_ProRatesEntryChargesNeverDoubleBillsBrokerage
// guards the one place charges could silently inflate: a partial exit books
// a new CLOSED row, and if it recomputed entry charges for that lot instead
// of pro-rating the parent's, every partial would bill the flat Rs.20
// brokerage again for an entry that only ever executed once.
func TestClosePartialAtPrice_ProRatesEntryChargesNeverDoubleBillsBrokerage(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	userID := testAccount(t, pool, 100000, 250000)
	instID := testInstrumentID(t, pool)
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM paper_trades WHERE user_id=$1::uuid`, userID) })

	// Open a 130-unit (2-lot) option position carrying a known entry cost.
	const entryCharges = 50.0
	var tradeID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO paper_trades (user_id, instrument_id, side, product_type, quantity, entry_price, entry_time, status, source, entry_charges)
		VALUES ($1::uuid, $2, 'BUY', 'DELIVERY', 130, 100, now(), 'OPEN', 'options-algo', $3) RETURNING id`,
		userID, instID, entryCharges).Scan(&tradeID); err != nil {
		t.Fatalf("seed open trade: %v", err)
	}

	parent := Trade{
		ID: tradeID, userID: userID, InstrumentID: instID, Source: SourceOptionsAlgo,
		Side: SideBuy, ProductType: ProductDelivery, OptionType: "CE",
		Quantity: 130, EntryPrice: 100, EntryCharges: entryCharges,
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := closePartialAtPrice(ctx, tx, parent, 65, 150); err != nil { // exit half
		t.Fatalf("closePartialAtPrice: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var lotEntry, parentEntry float64
	if err := pool.QueryRow(ctx,
		`SELECT entry_charges FROM paper_trades WHERE user_id=$1::uuid AND status='CLOSED'`, userID).Scan(&lotEntry); err != nil {
		t.Fatalf("read lot: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT entry_charges FROM paper_trades WHERE id=$1`, tradeID).Scan(&parentEntry); err != nil {
		t.Fatalf("read parent: %v", err)
	}

	// Half the quantity exited => half the entry cost moves with it.
	if math.Abs(lotEntry-entryCharges/2) > 0.0001 {
		t.Errorf("closed lot entry_charges = %.4f, want %.4f (half of the parent's)", lotEntry, entryCharges/2)
	}
	// And the two must still sum to exactly what was paid — no invention.
	if math.Abs((lotEntry+parentEntry)-entryCharges) > 0.0001 {
		t.Errorf("lot (%.4f) + parent (%.4f) = %.4f, want exactly the original %.4f — charges must never be created or destroyed by a partial exit",
			lotEntry, parentEntry, lotEntry+parentEntry, entryCharges)
	}
}

// TestSetAlgoEnabled_AndAlgoEnabledUserIDs covers the per-user auto-trading
// toggle (replaces the old single-account OPTIONS_ALGO_USER_EMAIL env var):
// flipping one account's flag must not affect another's, and
// AlgoEnabledUserIDs must return exactly the accounts currently opted in.
func TestSetAlgoEnabled_AndAlgoEnabledUserIDs(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	s := &Service{pool: pool}

	onID := testAccount(t, pool, 0, 0)
	offID := testAccount(t, pool, 0, 0)

	acct, err := s.SetAlgoEnabled(ctx, onID, true)
	if err != nil {
		t.Fatalf("SetAlgoEnabled(true): %v", err)
	}
	if !acct.AlgoEnabled {
		t.Fatal("expected AlgoEnabled=true in SetAlgoEnabled's returned account")
	}

	got, err := s.GetAccount(ctx, onID)
	if err != nil {
		t.Fatalf("GetAccount(onID): %v", err)
	}
	if !got.AlgoEnabled {
		t.Fatal("expected GetAccount to reflect algo_enabled=true after SetAlgoEnabled")
	}
	got, err = s.GetAccount(ctx, offID)
	if err != nil {
		t.Fatalf("GetAccount(offID): %v", err)
	}
	if got.AlgoEnabled {
		t.Fatal("expected the second (untouched) account to stay algo_enabled=false")
	}

	ids, err := s.AlgoEnabledUserIDs(ctx)
	if err != nil {
		t.Fatalf("AlgoEnabledUserIDs: %v", err)
	}
	foundOn, foundOff := false, false
	for _, id := range ids {
		if id == onID {
			foundOn = true
		}
		if id == offID {
			foundOff = true
		}
	}
	if !foundOn {
		t.Error("expected the enabled account to appear in AlgoEnabledUserIDs")
	}
	if foundOff {
		t.Error("expected the disabled account to NOT appear in AlgoEnabledUserIDs")
	}

	if _, err := s.SetAlgoEnabled(ctx, onID, false); err != nil {
		t.Fatalf("SetAlgoEnabled(false): %v", err)
	}
	ids, err = s.AlgoEnabledUserIDs(ctx)
	if err != nil {
		t.Fatalf("AlgoEnabledUserIDs after turning off: %v", err)
	}
	for _, id := range ids {
		if id == onID {
			t.Fatal("expected the account to disappear from AlgoEnabledUserIDs after being turned off")
		}
	}
}

// TestLockAccountForUpdate_BlocksConcurrentReader is the regression test for
// a real concurrency bug caught during review: OpenPosition/ConvertToDelivery
// used to check "sufficient funds" against an unlocked, pre-transaction
// balance read — so two concurrent orders for the same user could both pass
// the check against the same stale balance and both debit, overdrawing
// below zero. lockAccountForUpdate's FOR UPDATE read is supposed to close
// that by serializing concurrent callers on the same account row; this
// proves it actually blocks (and sees the post-commit balance), not just
// that it compiles.
func TestLockAccountForUpdate_BlocksConcurrentReader(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	userID := testAccount(t, pool, 1000, 0)
	s := &Service{}

	tx1, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	defer tx1.Rollback(ctx) //nolint:errcheck

	if _, err := s.lockAccountForUpdate(ctx, tx1, userID); err != nil {
		t.Fatalf("tx1 lock: %v", err)
	}
	// tx1 now holds the row lock. Debit it, but don't commit yet — a second
	// caller's lockAccountForUpdate must not be able to read past this.
	if err := debitBalance(ctx, tx1, userID, "web", 400); err != nil {
		t.Fatalf("tx1 debit: %v", err)
	}

	tx2Started := make(chan struct{})
	tx2Done := make(chan Account, 1)
	go func() {
		tx2, err := pool.Begin(ctx)
		if err != nil {
			t.Errorf("begin tx2: %v", err)
			close(tx2Started)
			return
		}
		defer tx2.Rollback(ctx) //nolint:errcheck
		close(tx2Started)
		acct, err := s.lockAccountForUpdate(ctx, tx2, userID)
		if err != nil {
			t.Errorf("tx2 lock: %v", err)
			return
		}
		tx2Done <- acct
	}()

	<-tx2Started
	select {
	case <-tx2Done:
		t.Fatal("tx2's lockAccountForUpdate returned before tx1 committed — the row lock isn't actually blocking concurrent callers")
	case <-time.After(300 * time.Millisecond):
		// expected: tx2 is blocked waiting on tx1's row lock.
	}

	if err := tx1.Commit(ctx); err != nil {
		t.Fatalf("commit tx1: %v", err)
	}

	select {
	case acct := <-tx2Done:
		if acct.CashBalance != 600 {
			t.Errorf("tx2 saw cash_balance=%v after tx1 committed, want 600 (1000-400) — must see tx1's debit, not a stale pre-commit value", acct.CashBalance)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tx2 never unblocked after tx1 committed")
	}
}
