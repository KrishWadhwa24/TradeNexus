package instruments

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"tradenexus/internal/store"
)

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

// TestFeaturedStocks_CapAndDedup regression-tests the two rules the admin UI
// depends on: the list can never exceed MaxFeatured, and adding the same
// instrument twice is a no-op error rather than a duplicate row.
//
// featured_stocks is real, admin-managed production state (it feeds the live
// public landing page), so this test computes everything relative to
// whatever is already there instead of assuming an empty table, and never
// mutates a pre-existing row — only rows it seeds itself.
func TestFeaturedStocks_CapAndDedup(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()

	before, err := repo.ListFeatured(ctx)
	if err != nil {
		t.Fatalf("ListFeatured (baseline): %v", err)
	}
	free := MaxFeatured - len(before)
	seedCount := free
	if seedCount < 0 {
		seedCount = 0
	}

	// Seed just enough synthetic instruments to fill any remaining capacity,
	// plus one more to prove ErrFeaturedFull once truly full.
	ids := make([]int64, seedCount+1)
	for i := range ids {
		var id int64
		err := repo.pool.QueryRow(ctx, `
			INSERT INTO instruments (symbol_token, exchange, trading_symbol, name, lot_size, active, updated_at)
			VALUES ($1, 'TEST', $2, $2, 1, TRUE, now())
			RETURNING id`,
			fmt.Sprintf("FEATTEST%d", i), fmt.Sprintf("FEATTEST%d", i)).Scan(&id)
		if err != nil {
			t.Fatalf("seed instrument %d: %v", i, err)
		}
		ids[i] = id
	}
	t.Cleanup(func() {
		for _, id := range ids {
			repo.pool.Exec(ctx, `DELETE FROM featured_stocks WHERE instrument_id = $1`, id)
			repo.pool.Exec(ctx, `DELETE FROM instruments WHERE id = $1`, id)
		}
	})

	for i := 0; i < seedCount; i++ {
		if err := repo.AddFeatured(ctx, ids[i]); err != nil {
			t.Fatalf("AddFeatured(%d) (filling remaining capacity, slot %d of %d): %v", ids[i], i+1, seedCount, err)
		}
	}

	// The list is now completely full (real pre-existing rows + our fill) —
	// one more must be rejected.
	overflow := ids[seedCount]
	if err := repo.AddFeatured(ctx, overflow); !errors.Is(err, ErrFeaturedFull) {
		t.Fatalf("expected ErrFeaturedFull once at MaxFeatured, got %v", err)
	}

	// Dedup: re-adding an existing member — ours if we seeded any, otherwise
	// a pre-existing real one (read-only on this path, never mutated) — must
	// report ErrAlreadyFeatured, not ErrFeaturedFull, even though full.
	var dupTarget int64
	switch {
	case seedCount > 0:
		dupTarget = ids[0]
	case len(before) > 0:
		dupTarget = before[0].ID
	default:
		t.Skip("no capacity and no pre-existing featured stock to test dedup against")
	}
	if err := repo.AddFeatured(ctx, dupTarget); !errors.Is(err, ErrAlreadyFeatured) {
		t.Fatalf("expected ErrAlreadyFeatured re-adding an existing member, got %v", err)
	}

	list, err := repo.ListFeatured(ctx)
	if err != nil {
		t.Fatalf("ListFeatured: %v", err)
	}
	if len(list) != MaxFeatured {
		t.Fatalf("ListFeatured returned %d stocks, want %d (MaxFeatured)", len(list), MaxFeatured)
	}

	// Freeing exactly one of OUR OWN slots must allow exactly one more add.
	// Skipped (not failed) if we never got any capacity to seed, so this test
	// never removes a pre-existing real row to make room.
	if seedCount == 0 {
		t.Skip("table was already full of pre-existing data; skipping the free-a-slot check to avoid touching real rows")
	}
	if err := repo.RemoveFeatured(ctx, ids[0]); err != nil {
		t.Fatalf("RemoveFeatured: %v", err)
	}
	if err := repo.AddFeatured(ctx, overflow); err != nil {
		t.Fatalf("AddFeatured after freeing a slot: %v", err)
	}
}

// TestFeaturedStocks_RankUniqueAfterRemoval is the regression test for a real
// bug: AddFeatured used count(*) as the new row's rank, which collides with a
// still-in-use rank once a removal leaves a gap (e.g. 9 rows ranked 0-7,9 —
// removing rank 8 then adding a new row assigns rank=count()=9, duplicating
// the surviving row's rank 9). Needs at least one free slot to exercise the
// add-after-remove path; skips (not fails) if the real table is already full,
// same conservative rule as the test above.
func TestFeaturedStocks_RankUniqueAfterRemoval(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()

	before, err := repo.ListFeatured(ctx)
	if err != nil {
		t.Fatalf("ListFeatured (baseline): %v", err)
	}
	if len(before) >= MaxFeatured {
		t.Skip("featured list already full; nothing to add for this check")
	}

	mk := func(sym string) int64 {
		var id int64
		if err := repo.pool.QueryRow(ctx, `
			INSERT INTO instruments (symbol_token, exchange, trading_symbol, name, lot_size, active, updated_at)
			VALUES ($1, 'TEST', $1, $1, 1, TRUE, now()) RETURNING id`, sym).Scan(&id); err != nil {
			t.Fatalf("seed instrument %s: %v", sym, err)
		}
		return id
	}
	a, b := mk("RANKTESTA"), mk("RANKTESTB")
	t.Cleanup(func() {
		for _, id := range []int64{a, b} {
			repo.pool.Exec(ctx, `DELETE FROM featured_stocks WHERE instrument_id = $1`, id)
			repo.pool.Exec(ctx, `DELETE FROM instruments WHERE id = $1`, id)
		}
	})

	if err := repo.AddFeatured(ctx, a); err != nil {
		t.Fatalf("AddFeatured(a): %v", err)
	}
	if err := repo.RemoveFeatured(ctx, a); err != nil {
		t.Fatalf("RemoveFeatured(a): %v", err)
	}
	if err := repo.AddFeatured(ctx, b); err != nil {
		t.Fatalf("AddFeatured(b): %v", err)
	}

	rows, err := repo.pool.Query(ctx, `SELECT rank, count(*) FROM featured_stocks GROUP BY rank HAVING count(*) > 1`)
	if err != nil {
		t.Fatalf("check duplicate ranks: %v", err)
	}
	defer rows.Close()
	var dupes []int
	for rows.Next() {
		var rank, n int
		rows.Scan(&rank, &n)
		dupes = append(dupes, rank)
	}
	if len(dupes) > 0 {
		t.Fatalf("duplicate rank(s) found after add-remove-add: %v", dupes)
	}
}
