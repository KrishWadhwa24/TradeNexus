package engine

import (
	"testing"
	"time"

	"tradenexus/internal/market"
)

func day(y int, m time.Month, d int, close float64) market.Candle {
	return market.Candle{
		Time:  time.Date(y, m, d, 0, 0, 0, 0, market.IST),
		Open:  close, High: close, Low: close, Close: close, Volume: 100,
	}
}

func TestAppendToday_Empty(t *testing.T) {
	td := day(2026, 7, 10, 100)
	got := appendToday(nil, td)
	if len(got) != 1 || got[0].Close != 100 {
		t.Fatalf("expected [today], got %+v", got)
	}
}

func TestAppendToday_NewDayAppends(t *testing.T) {
	daily := []market.Candle{day(2026, 7, 8, 90), day(2026, 7, 9, 95)}
	td := day(2026, 7, 10, 102) // later date
	got := appendToday(daily, td)
	if len(got) != 3 {
		t.Fatalf("expected 3 candles after append, got %d", len(got))
	}
	if got[2].Close != 102 {
		t.Fatalf("last candle should be today's, got %v", got[2].Close)
	}
}

func TestAppendToday_SameDayReplaces(t *testing.T) {
	daily := []market.Candle{day(2026, 7, 9, 95), day(2026, 7, 10, 100)}
	td := day(2026, 7, 10, 108) // same date, fresher close
	got := appendToday(daily, td)
	if len(got) != 2 {
		t.Fatalf("same-day candle must replace, not append; got %d", len(got))
	}
	if got[1].Close != 108 {
		t.Fatalf("last candle should be replaced with fresher close, got %v", got[1].Close)
	}
}

func TestAppendToday_StaleIgnored(t *testing.T) {
	daily := []market.Candle{day(2026, 7, 9, 95), day(2026, 7, 10, 100)}
	td := day(2026, 7, 8, 80) // older than last — should not corrupt the series
	got := appendToday(daily, td)
	if len(got) != 2 || got[1].Close != 100 {
		t.Fatalf("stale candle must be ignored, got %+v", got)
	}
}
