package calendar

import (
	"testing"
	"time"
)

var loc = time.UTC

func d(y int, m time.Month, day int) time.Time {
	return time.Date(y, m, day, 0, 0, 0, 0, loc)
}

func dt(y int, m time.Month, day, hh, mm int) time.Time {
	return time.Date(y, m, day, hh, mm, 0, 0, loc)
}

func TestLastFinalizedTradingDay(t *testing.T) {
	cal := New(nil) // weekends-only
	const buf = 15  // finalized at 15:45

	// 2026-07-13 Mon, 2026-07-10 Fri, 2026-07-11 Sat, 2026-07-12 Sun.
	// Monday pre-close → previous finalized day is Friday.
	if got := cal.LastFinalizedTradingDay(dt(2026, 7, 13, 10, 0), buf); !got.Equal(d(2026, 7, 10)) {
		t.Errorf("Mon 10:00 → want Fri 07-10, got %v", got.Format("2006-01-02"))
	}
	// Monday just before close+buffer (15:40 < 15:45) → still Friday.
	if got := cal.LastFinalizedTradingDay(dt(2026, 7, 13, 15, 40), buf); !got.Equal(d(2026, 7, 10)) {
		t.Errorf("Mon 15:40 → want Fri 07-10, got %v", got.Format("2006-01-02"))
	}
	// Monday after close+buffer (16:00) → today (Monday) is finalized.
	if got := cal.LastFinalizedTradingDay(dt(2026, 7, 13, 16, 0), buf); !got.Equal(d(2026, 7, 13)) {
		t.Errorf("Mon 16:00 → want Mon 07-13, got %v", got.Format("2006-01-02"))
	}
	// Saturday any time → previous trading day Friday.
	if got := cal.LastFinalizedTradingDay(dt(2026, 7, 11, 18, 0), buf); !got.Equal(d(2026, 7, 10)) {
		t.Errorf("Sat → want Fri 07-10, got %v", got.Format("2006-01-02"))
	}

	// With a holiday on Monday 07-13: post-close should skip it to Friday.
	calH := New([]time.Time{d(2026, 7, 13)})
	if got := calH.LastFinalizedTradingDay(dt(2026, 7, 13, 16, 0), buf); !got.Equal(d(2026, 7, 10)) {
		t.Errorf("holiday Mon 16:00 → want Fri 07-10, got %v", got.Format("2006-01-02"))
	}
}

func TestIsTradingDay(t *testing.T) {
	// 2024-01-26 is a Friday (holiday), 27=Sat, 28=Sun, 29=Mon.
	cal := New([]time.Time{d(2024, 1, 26)})
	if cal.IsTradingDay(d(2024, 1, 26)) {
		t.Error("holiday should not be a trading day")
	}
	if cal.IsTradingDay(d(2024, 1, 27)) || cal.IsTradingDay(d(2024, 1, 28)) {
		t.Error("weekend should not be a trading day")
	}
	if !cal.IsTradingDay(d(2024, 1, 29)) {
		t.Error("Monday (non-holiday) should be a trading day")
	}
}

func TestTradingDays_SkipsWeekendsAndHolidays(t *testing.T) {
	cal := New([]time.Time{d(2024, 1, 1)}) // Mon holiday
	// Week of Jan 1 (Mon) .. Jan 7 (Sun): trading days = Tue..Fri = 4.
	got := cal.TradingDays(d(2024, 1, 1), d(2024, 1, 7))
	if len(got) != 4 {
		t.Fatalf("expected 4 trading days, got %d: %v", len(got), got)
	}
}

func TestMissingTradingDays(t *testing.T) {
	cal := New(nil)
	// Have data through Wed Jan 3; ask up to Fri Jan 5. Missing = Thu, Fri.
	have := map[string]bool{
		"2024-01-01": true, "2024-01-02": true, "2024-01-03": true,
	}
	missing := cal.MissingTradingDays(d(2024, 1, 3), d(2024, 1, 5), have)
	if len(missing) != 2 {
		t.Fatalf("expected 2 missing (Thu, Fri), got %d: %v", len(missing), missing)
	}
	// A weekend-only gap should yield nothing.
	missing = cal.MissingTradingDays(d(2024, 1, 5), d(2024, 1, 7), have)
	if len(missing) != 0 {
		t.Fatalf("weekend gap should be empty, got %v", missing)
	}
}
