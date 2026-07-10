package scanner

import "tradenexus/internal/market"

// Report is the full scan outcome for one instrument across all timeframes.
type Report struct {
	DailyPine   PineSignal    `json:"daily_pine"`
	WeeklyPine  PineSignal    `json:"weekly_pine"`
	MonthlyPine PineSignal    `json:"monthly_pine"`
	Weekly      WeeklyResult  `json:"weekly_scanners"`
	Patterns    PatternReport `json:"patterns"`
}

// Run evaluates the Pine strategy on daily/weekly/monthly candles and the four
// weekly scanners on weekly candles.
//
// Timeframe rule (from the spec): the DAILY Pine only fires on a closed daily
// candle (the caller passes closed daily bars). WEEKLY/MONTHLY may fire on the
// latest (possibly forming) bar — that's just the last element of those slices.
// The dailyConfirmed/weeklyConfirmed/monthlyConfirmed flags say whether the last
// candle of each series is a closed bar. Pine and the weekly scanners always use
// the latest (possibly forming) bar; only the pattern scanners honor these flags
// so they never fire on a partially-formed candle.
func Run(daily, weekly, monthly []market.Candle, cfg PineConfig,
	dailyConfirmed, weeklyConfirmed, monthlyConfirmed bool) Report {
	return Report{
		DailyPine:   ScanPine(daily, cfg),
		WeeklyPine:  ScanPine(weekly, cfg),
		MonthlyPine: ScanPine(monthly, cfg),
		Weekly:      ScanWeekly(weekly),
		Patterns: PatternReport{
			Daily:   ScanPatternTimeframe(daily, true, true, dailyConfirmed),
			Weekly:  ScanPatternTimeframe(weekly, true, true, weeklyConfirmed),
			Monthly: ScanPatternTimeframe(monthly, false, false, monthlyConfirmed),
		},
	}
}
