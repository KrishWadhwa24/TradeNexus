package intraday

import (
	"testing"
	"time"

	"github.com/rs/zerolog"

	"tradenexus/internal/calendar"
	"tradenexus/internal/market"
)

func TestKey(t *testing.T) {
	if got := key(1384); got != "intraday:candle:1384" {
		t.Fatalf("key(1384) = %q", got)
	}
}

func TestMarketOpen(t *testing.T) {
	// Weekends-only calendar (no DB): NSE hours 09:15–15:30 IST, Mon–Fri.
	cal := calendar.NewService(nil, "NSE")
	c := New(nil, nil, nil, nil, cal, 0, zerolog.Nop())

	// 2026-07-13 is a Monday.
	monMidday := time.Date(2026, 7, 13, 12, 0, 0, 0, market.IST)
	monPreOpen := time.Date(2026, 7, 13, 8, 0, 0, 0, market.IST)
	sat := time.Date(2026, 7, 11, 12, 0, 0, 0, market.IST)

	if !c.MarketOpen(monMidday) {
		t.Error("Monday 12:00 IST should be open")
	}
	if c.MarketOpen(monPreOpen) {
		t.Error("Monday 08:00 IST (pre-open) should be closed")
	}
	if c.MarketOpen(sat) {
		t.Error("Saturday should be closed")
	}
}
