package fiidii

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"tradenexus/internal/store"
)

// fakeBroadcaster counts sends and can be told to fail the next call.
type fakeBroadcaster struct {
	mu    sync.Mutex
	calls int
	fail  bool
}

func (f *fakeBroadcaster) Broadcast(ctx context.Context, text string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return 0, fmt.Errorf("broadcast failed")
	}
	f.calls++
	return 1, nil
}

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

// TestMaybeSendAlert_SkipsAfterRestart is the regression test for the bug
// report: on a server restart, the in-memory alertedDate resets, so a stale
// "ready" auto-alert would re-fire and Telegram the same date twice. The DB
// ledger must catch that even with a brand new Service (i.e. no shared
// in-memory state) hitting the same date.
func TestMaybeSendAlert_SkipsAfterRestart(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	testDate := "05-Jan-2020" // arbitrary past NSE-format date, cleaned up below
	day, err := parseNSEDate(testDate)
	if err != nil {
		t.Fatalf("parseNSEDate: %v", err)
	}
	_, _ = repo.pool.Exec(ctx, `DELETE FROM fiidii_alerted WHERE trade_date = $1`, day)
	t.Cleanup(func() { repo.pool.Exec(ctx, `DELETE FROM fiidii_alerted WHERE trade_date = $1`, day) })

	snap := Snapshot{Date: testDate, FetchedAt: time.Now()}
	dateISO := "2020-01-05"

	// --- "process 1": data ready + reconcile done -> should send once.
	bc1 := &fakeBroadcaster{}
	s1 := New(nil, bc1, nil, repo, zerolog.Nop())
	s1.latest = snap
	s1.readyDate = dateISO
	s1.reconcileDoneDate = dateISO
	s1.maybeSendAlert()
	if bc1.calls != 1 {
		t.Fatalf("process 1: want 1 broadcast, got %d", bc1.calls)
	}
	done, err := repo.AlreadyAlerted(ctx, day)
	if err != nil {
		t.Fatalf("AlreadyAlerted: %v", err)
	}
	if !done {
		t.Fatalf("expected ledger to record the alert as sent")
	}

	// --- "process 2": brand new Service (simulates a restart), same date
	// becomes ready again -> must NOT re-send.
	bc2 := &fakeBroadcaster{}
	s2 := New(nil, bc2, nil, repo, zerolog.Nop())
	s2.latest = snap
	s2.readyDate = dateISO
	s2.reconcileDoneDate = dateISO
	s2.maybeSendAlert()
	if bc2.calls != 0 {
		t.Fatalf("process 2 (post-restart): want 0 broadcasts, got %d — duplicate alert sent!", bc2.calls)
	}
}

// TestMaybeSendAlert_RetriesAfterBroadcastFailure ensures a failed Telegram
// send doesn't get marked in the ledger, so the next trigger can retry.
func TestMaybeSendAlert_RetriesAfterBroadcastFailure(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	testDate := "06-Jan-2020"
	day, err := parseNSEDate(testDate)
	if err != nil {
		t.Fatalf("parseNSEDate: %v", err)
	}
	_, _ = repo.pool.Exec(ctx, `DELETE FROM fiidii_alerted WHERE trade_date = $1`, day)
	t.Cleanup(func() { repo.pool.Exec(ctx, `DELETE FROM fiidii_alerted WHERE trade_date = $1`, day) })

	snap := Snapshot{Date: testDate, FetchedAt: time.Now()}
	dateISO := "2020-01-06"

	bc := &fakeBroadcaster{fail: true}
	s := New(nil, bc, nil, repo, zerolog.Nop())
	s.latest = snap
	s.readyDate = dateISO
	s.reconcileDoneDate = dateISO
	s.maybeSendAlert()
	if bc.calls != 0 {
		t.Fatalf("expected failed broadcast to not count as sent, got %d calls", bc.calls)
	}
	if done, _ := repo.AlreadyAlerted(ctx, day); done {
		t.Fatalf("ledger should not be marked when broadcast fails")
	}

	// retry: same Service, broadcaster now succeeds.
	bc.fail = false
	s.maybeSendAlert()
	if bc.calls != 1 {
		t.Fatalf("expected retry to send once broadcaster recovers, got %d calls", bc.calls)
	}
	if done, _ := repo.AlreadyAlerted(ctx, day); !done {
		t.Fatalf("ledger should be marked after the successful retry")
	}
}
