package optionsalgo

import (
	"time"

	"tradenexus/internal/market"
	"tradenexus/internal/paper"
)

// netPnL is a closed trade's P&L after the real statutory/broker charges
// booked against it. The circuit breakers below must use this, not the gross
// t.PnL: the account's cash balance already has charges deducted, so a gross
// comparison lets the daily/weekly loss limits trip LATE by exactly the
// accumulated charges — the kill switch would be looser than the percentage
// actually configured. Charges are 0 for equity and for trades predating
// them, so this is identical to gross in those cases.
func netPnL(t paper.Trade) float64 {
	return t.PnL - t.EntryCharges - t.ExitCharges
}

// dailyAlgoPnL sums realized NET P&L for algo-sourced trades CLOSED on the
// same IST calendar day as `now` — a trade's loss/profit counts on the day
// it was closed, not opened, matching "how much have I lost today."
func dailyAlgoPnL(trades []paper.Trade, now time.Time) float64 {
	var sum float64
	for _, t := range trades {
		if t.Source != paper.SourceOptionsAlgo || t.Status != "CLOSED" || t.ExitTime == nil {
			continue
		}
		if market.SameISTDate(*t.ExitTime, now) {
			sum += netPnL(t)
		}
	}
	return sum
}

// weeklyAlgoPnL sums realized NET P&L (see netPnL) for algo-sourced trades
// CLOSED in the same ISO week (IST) as `now`.
func weeklyAlgoPnL(trades []paper.Trade, now time.Time) float64 {
	nowYear, nowWeek := now.In(market.IST).ISOWeek()
	var sum float64
	for _, t := range trades {
		if t.Source != paper.SourceOptionsAlgo || t.Status != "CLOSED" || t.ExitTime == nil {
			continue
		}
		y, w := t.ExitTime.In(market.IST).ISOWeek()
		if y == nowYear && w == nowWeek {
			sum += netPnL(t)
		}
	}
	return sum
}
