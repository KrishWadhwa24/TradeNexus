package scanner

import "tradenexus/internal/market"

// Report is the full scan outcome for one instrument across all timeframes.
type Report struct {
	DailyPine   PineSignal   `json:"daily_pine"`
	WeeklyPine  PineSignal   `json:"weekly_pine"`
	MonthlyPine PineSignal   `json:"monthly_pine"`
	Weekly      WeeklyResult `json:"weekly_scanners"`
}

// Run evaluates the Pine strategy on daily/weekly/monthly candles and the four
// weekly scanners on weekly candles.
//
// Timeframe rule (from the spec): the DAILY Pine only fires on a closed daily
// candle (the caller passes closed daily bars). WEEKLY/MONTHLY may fire on the
// latest (possibly forming) bar — that's just the last element of those slices.
func Run(daily, weekly, monthly []market.Candle, cfg PineConfig) Report {
	return Report{
		DailyPine:   ScanPine(daily, cfg),
		WeeklyPine:  ScanPine(weekly, cfg),
		MonthlyPine: ScanPine(monthly, cfg),
		Weekly:      ScanWeekly(weekly),
	}
}
