package optionsalgo

import (
	"time"

	"tradenexus/internal/market"
	"tradenexus/internal/paper"
)

// dailyAlgoPnL sums realized P&L for algo-sourced trades CLOSED on the same
// IST calendar day as `now` — a trade's loss/profit counts on the day it
// was closed, not opened, matching "how much have I lost today."
func dailyAlgoPnL(trades []paper.Trade, now time.Time) float64 {
	var sum float64
	for _, t := range trades {
		if t.Source != paper.SourceOptionsAlgo || t.Status != "CLOSED" || t.ExitTime == nil {
			continue
		}
		if market.SameISTDate(*t.ExitTime, now) {
			sum += t.PnL
		}
	}
	return sum
}

// weeklyAlgoPnL sums realized P&L for algo-sourced trades CLOSED in the same
// ISO week (IST) as `now`.
func weeklyAlgoPnL(trades []paper.Trade, now time.Time) float64 {
	nowYear, nowWeek := now.In(market.IST).ISOWeek()
	var sum float64
	for _, t := range trades {
		if t.Source != paper.SourceOptionsAlgo || t.Status != "CLOSED" || t.ExitTime == nil {
			continue
		}
		y, w := t.ExitTime.In(market.IST).ISOWeek()
		if y == nowYear && w == nowWeek {
			sum += t.PnL
		}
	}
	return sum
}
