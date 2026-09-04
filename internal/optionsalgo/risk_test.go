package optionsalgo

import (
	"testing"
	"time"

	"tradenexus/internal/market"
	"tradenexus/internal/paper"
)

func timePtr(t time.Time) *time.Time { return &t }

func TestDailyAlgoPnL(t *testing.T) {
	now := time.Date(2026, 9, 5, 14, 0, 0, 0, market.IST)
	today := time.Date(2026, 9, 5, 10, 0, 0, 0, market.IST)
	yesterday := time.Date(2026, 9, 4, 10, 0, 0, 0, market.IST)
	trades := []paper.Trade{
		{Source: paper.SourceOptionsAlgo, Status: "CLOSED", ExitTime: timePtr(today), PnL: -500},
		{Source: paper.SourceOptionsAlgo, Status: "CLOSED", ExitTime: timePtr(today), PnL: 200},
		{Source: paper.SourceOptionsAlgo, Status: "CLOSED", ExitTime: timePtr(yesterday), PnL: -9999}, // different day, excluded
		{Source: "web", Status: "CLOSED", ExitTime: timePtr(today), PnL: -9999},                       // not algo, excluded
		{Source: paper.SourceOptionsAlgo, Status: "OPEN", ExitTime: nil, PnL: 0},                      // still open, excluded
	}
	got := dailyAlgoPnL(trades, now)
	if got != -300 {
		t.Errorf("dailyAlgoPnL = %v, want -300 (only today's two algo closes)", got)
	}
}

func TestWeeklyAlgoPnL(t *testing.T) {
	// 2026-09-05 is a Saturday; use a definite same-week weekday pair.
	now := time.Date(2026, 9, 3, 14, 0, 0, 0, market.IST)      // Thursday
	sameWeek := time.Date(2026, 9, 1, 10, 0, 0, 0, market.IST) // Tuesday, same ISO week
	lastWeek := time.Date(2026, 8, 25, 10, 0, 0, 0, market.IST)
	trades := []paper.Trade{
		{Source: paper.SourceOptionsAlgo, Status: "CLOSED", ExitTime: timePtr(now), PnL: -1000},
		{Source: paper.SourceOptionsAlgo, Status: "CLOSED", ExitTime: timePtr(sameWeek), PnL: 300},
		{Source: paper.SourceOptionsAlgo, Status: "CLOSED", ExitTime: timePtr(lastWeek), PnL: -9999},
	}
	got := weeklyAlgoPnL(trades, now)
	if got != -700 {
		t.Errorf("weeklyAlgoPnL = %v, want -700 (this week's two closes only)", got)
	}
}
