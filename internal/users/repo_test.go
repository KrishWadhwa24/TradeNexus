package users

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"tradenexus/internal/store"
)

func TestDeleteWatchlist_CascadesItems(t *testing.T) {
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
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("ping postgres: %v", err)
	}

	repo := NewRepo(pool)
	email := fmt.Sprintf("watchlist-delete-%d@example.com", time.Now().UnixNano())
	userID, err := repo.CreateUser(ctx, email)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	var instrumentID int64
	if err := pool.QueryRow(ctx, `
		INSERT INTO instruments (symbol_token, exchange, trading_symbol, name)
		VALUES ($1, $2, $3, $4)
		RETURNING id`, fmt.Sprintf("tok-%d", time.Now().UnixNano()), "NSE", "TEST-EQ", "TEST").Scan(&instrumentID); err != nil {
		t.Fatalf("insert instrument: %v", err)
	}

	watchlistID, err := repo.CreateWatchlist(ctx, userID, "My Watchlist")
	if err != nil {
		t.Fatalf("CreateWatchlist: %v", err)
	}
	if err := repo.AddWatchlistItem(ctx, watchlistID, instrumentID); err != nil {
		t.Fatalf("AddWatchlistItem: %v", err)
	}

	watchlists, err := repo.ListWatchlists(ctx, userID)
	if err != nil {
		t.Fatalf("ListWatchlists before delete: %v", err)
	}
	if len(watchlists) != 1 || len(watchlists[0].InstrumentIDs) != 1 {
		t.Fatalf("expected one watchlist with one item before delete, got %#v", watchlists)
	}

	if err := repo.DeleteWatchlist(ctx, userID, watchlistID); err != nil {
		t.Fatalf("DeleteWatchlist: %v", err)
	}

	watchlists, err = repo.ListWatchlists(ctx, userID)
	if err != nil {
		t.Fatalf("ListWatchlists after delete: %v", err)
	}
	if len(watchlists) != 0 {
		t.Fatalf("expected no watchlists after delete, got %#v", watchlists)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM watchlist_items WHERE watchlist_id = $1::uuid`, watchlistID).Scan(&count); err != nil {
		t.Fatalf("count watchlist_items: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected watchlist items to cascade delete, got %d", count)
	}
}
