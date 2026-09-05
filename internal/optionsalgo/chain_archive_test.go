package optionsalgo

import (
	"context"
	"testing"
	"time"

	"tradenexus/internal/market"
)

func TestSnapshotRows_TruncatesToTheMinute(t *testing.T) {
	at := time.Date(2026, 9, 7, 10, 47, 38, 500_000_000, market.IST)
	rows := snapshotRows([]OptionQuote{{InstrumentID: 1, LTP: 100}}, at)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	want := time.Date(2026, 9, 7, 10, 47, 0, 0, market.IST)
	if !rows[0].SnapshotTime.Equal(want) {
		t.Errorf("SnapshotTime = %v, want %v — snapshots must land on a clean 1-minute grid so the upsert key is stable", rows[0].SnapshotTime, want)
	}
}

// TestSnapshotRows_MissingGreeksStoreNull is the important one: Angel's
// Greeks endpoint is unavailable outside market hours, and BuildOptionChain
// deliberately degrades to prices-only. Storing 0 in that case would be
// indistinguishable from a genuine deep-OTM delta of 0 to anything reading
// this back later — including a future backtest's strike selection.
func TestSnapshotRows_MissingGreeksStoreNull(t *testing.T) {
	at := time.Date(2026, 9, 7, 10, 47, 0, 0, market.IST)
	chain := []OptionQuote{
		{InstrumentID: 1, LTP: 100, Bid: 99, Ask: 101, Volume: 500, OpenInterest: 12345}, // greeks unavailable
		{InstrumentID: 2, LTP: 200, Delta: 0.61, IV: 14.2, Theta: -8.1, Gamma: 0.0004, Vega: 12.5},
	}
	rows := snapshotRows(chain, at)

	if rows[0].Delta != nil || rows[0].IV != nil {
		t.Error("expected NULL greeks when the Greeks call returned nothing, got a stored value")
	}
	// Prices must still be captured — a snapshot without greeks is still worth keeping.
	if rows[0].Bid != 99 || rows[0].Ask != 101 || rows[0].OpenInterest != 12345 {
		t.Errorf("prices/OI not captured on the greeks-less row: %+v", rows[0])
	}

	if rows[1].Delta == nil || *rows[1].Delta != 0.61 {
		t.Errorf("expected delta 0.61 stored, got %v", rows[1].Delta)
	}
	if rows[1].IV == nil || *rows[1].IV != 14.2 {
		t.Errorf("expected IV 14.2 stored, got %v", rows[1].IV)
	}
}

// TestInsertChainSnapshot_IsIdempotent proves re-archiving the same minute
// updates rather than duplicating or erroring — the polling tick and any
// manual/debug trigger can easily land within the same minute.
func TestInsertChainSnapshot_IsIdempotent(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	instID := testInstrumentID(t, repo)
	at := time.Date(2026, 1, 2, 10, 47, 0, 0, market.IST) // fixed past minute, won't collide with live data
	t.Cleanup(func() {
		repo.pool.Exec(ctx, `DELETE FROM option_chain_snapshots WHERE instrument_id=$1 AND snapshot_time=$2`, instID, at)
	})

	first := []ChainSnapshot{{InstrumentID: instID, SnapshotTime: at, LTP: 100, Bid: 99, Ask: 101, Volume: 10, OpenInterest: 500}}
	if _, err := repo.InsertChainSnapshot(ctx, first); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	// Same minute, moved prices — must overwrite, not duplicate or fail.
	second := []ChainSnapshot{{InstrumentID: instID, SnapshotTime: at, LTP: 111, Bid: 110, Ask: 112, Volume: 20, OpenInterest: 600}}
	if _, err := repo.InsertChainSnapshot(ctx, second); err != nil {
		t.Fatalf("re-insert same minute: %v", err)
	}

	var count int
	var ltp, bid float64
	if err := repo.pool.QueryRow(ctx,
		`SELECT count(*), max(ltp), max(bid) FROM option_chain_snapshots WHERE instrument_id=$1 AND snapshot_time=$2`,
		instID, at).Scan(&count, &ltp, &bid); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if count != 1 {
		t.Errorf("got %d rows for one instrument-minute, want exactly 1 (the upsert must not duplicate)", count)
	}
	if ltp != 111 || bid != 110 {
		t.Errorf("row = ltp %v bid %v, want the second snapshot's 111/110 (upsert must overwrite)", ltp, bid)
	}
}

func TestInsertChainSnapshot_EmptyIsNoOp(t *testing.T) {
	repo := testRepo(t)
	n, err := repo.InsertChainSnapshot(context.Background(), nil)
	if err != nil || n != 0 {
		t.Errorf("InsertChainSnapshot(nil) = (%d, %v), want (0, nil)", n, err)
	}
}
