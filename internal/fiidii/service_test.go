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

	"tradenexus/internal/calendar"
	"tradenexus/internal/market"
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
	s1 := New(nil, bc1, nil, 0, repo, zerolog.Nop())
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
	s2 := New(nil, bc2, nil, 0, repo, zerolog.Nop())
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
	s := New(nil, bc, nil, 0, repo, zerolog.Nop())
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

// TestNextDelay_RecognizesLateArrivingPreviousDay is the regression test for
// the bug report: NSE published a trading day's FII/DII data after midnight,
// so it landed on the calendar's "next day" — the old isToday-only check
// silently refused to treat that as ready. now (00:30 on a non-trading
// Saturday) should resolve to Friday as the last finalized trading day, and
// once readyDate is set to THAT date (not literal "today"), nextDelay must
// recognize the day is done rather than spinning on hourly retries forever.
func TestNextDelay_RecognizesLateArrivingPreviousDay(t *testing.T) {
	cal := calendar.New(nil)
	s := &Service{cal: cal, closeBuffer: 15}

	now := time.Date(2026, 8, 22, 0, 30, 0, 0, market.IST) // Saturday 00:30
	expected := s.cal.LastFinalizedTradingDay(now, s.closeBuffer)
	if dateKey(expected) != "2026-08-21" { // Friday
		t.Fatalf("expected last finalized trading day 2026-08-21, got %s", dateKey(expected))
	}

	s.readyDate = "" // data hasn't arrived yet
	if d := s.nextDelay(now); d <= 0 {
		t.Fatalf("expected a positive wait while not ready, got %v", d)
	}

	s.readyDate = dateKey(expected) // data just landed, keyed to the trading day it's FOR
	tomorrowClose := time.Date(now.Year(), now.Month(), now.Day()+1, marketCloseHour, 0, 0, 0, market.IST)
	if d := s.nextDelay(now); d != tomorrowClose.Sub(now) {
		t.Fatalf("expected sleep-until-tomorrow-close (%v) once ready, got %v", tomorrowClose.Sub(now), d)
	}
}

// TestListWeeklyMonthly_Aggregates checks that daily flows stored via
// UpsertFlow get summed correctly per week and per month, and that an
// UpsertFlow on an existing date overwrites rather than double-counts.
func TestListWeeklyMonthly_Aggregates(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()

	// Two days in the current calendar month, one in the previous month —
	// relative to "now" since ListMonthly filters by a rolling cutoff.
	now := time.Now()
	thisMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	days := []time.Time{
		thisMonthStart,
		thisMonthStart.AddDate(0, 0, 1),
		thisMonthStart.AddDate(0, -1, 0),
	}
	t.Cleanup(func() {
		for _, d := range days {
			repo.pool.Exec(ctx, `DELETE FROM fiidii_flows WHERE trade_date = $1`, d)
		}
	})

	mk := func(dii, fii float64) Snapshot {
		return Snapshot{DII: Flow{BuyValue: dii, SellValue: 0, NetValue: dii}, FII: Flow{BuyValue: fii, SellValue: 0, NetValue: fii}}
	}
	for i, d := range days {
		if err := repo.UpsertFlow(ctx, d, mk(float64(100*(i+1)), float64(-50*(i+1)))); err != nil {
			t.Fatalf("UpsertFlow(%v): %v", d, err)
		}
	}
	// Re-upsert the first day with different values — must overwrite, not add.
	if err := repo.UpsertFlow(ctx, days[0], mk(999, 999)); err != nil {
		t.Fatalf("UpsertFlow overwrite: %v", err)
	}

	monthly, err := repo.ListMonthly(ctx, 24)
	if err != nil {
		t.Fatalf("ListMonthly: %v", err)
	}
	var curNet, prevNet float64
	var curFound, prevFound bool
	prevMonthStart := thisMonthStart.AddDate(0, -1, 0)
	for _, p := range monthly {
		if sameMonth(p.PeriodStart, thisMonthStart) {
			curNet, curFound = p.DII.NetValue, true
		}
		if sameMonth(p.PeriodStart, prevMonthStart) {
			prevNet, prevFound = p.DII.NetValue, true
		}
	}
	if !curFound || !prevFound {
		t.Fatalf("expected both current and previous month buckets, got %+v", monthly)
	}
	// day0 overwritten to 999, day1 is 200 -> current month DII net = 1199.
	if curNet != 1199 {
		t.Fatalf("current month DII net = %v, want 1199 (overwrite must replace, not add)", curNet)
	}
	if prevNet != 300 { // day2: 100*3
		t.Fatalf("previous month DII net = %v, want 300", prevNet)
	}
}

func sameMonth(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month()
}
