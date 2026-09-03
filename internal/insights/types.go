// Package insights provides read-only cross-signal analytics: scanner
// performance (forward returns), a smart-money confluence board, and market
// breadth. It reads from the signals, promoter, deals and candle tables and
// maintains one long-lived table (signal_outcomes); it never mutates the data
// owned by other modules.
package insights

import "time"

// HorizonStat is the performance of a scanner at one forward horizon.
type HorizonStat struct {
	N         int     `json:"n"`          // matured samples
	AvgReturn float64 `json:"avg_return"` // avg directional return %, >0 = in signal's favour
	WinRate   float64 `json:"win_rate"`   // % of samples with return > 0
}

// ScannerPerf is one scanner+timeframe's forward-return profile.
type ScannerPerf struct {
	Source    string      `json:"source"`
	Timeframe string      `json:"timeframe"`
	Label     string      `json:"label"`
	D5        HorizonStat `json:"d5"`
	D10       HorizonStat `json:"d10"`
	D20       HorizonStat `json:"d20"`
	D30       HorizonStat `json:"d30"`
}

// BreadthPoint is one day's bullish/bearish signal counts.
type BreadthPoint struct {
	Date  time.Time `json:"date"`
	Buys  int       `json:"buys"`
	Sells int       `json:"sells"`
}

// ConfluenceStock is one stock on the smart-money confluence board — the
// bullish sources that fired for it within the confluence window.
type ConfluenceStock struct {
	Symbol      string   `json:"symbol"`
	Name        string   `json:"name"`
	Score       int      `json:"score"` // number of distinct bullish sources
	ScannerBuy  bool     `json:"scanner_buy"`
	PromoterBuy bool     `json:"promoter_buy"`
	BulkBuy     bool     `json:"bulk_buy"`
	BlockBuy    bool     `json:"block_buy"`
	Sources     []string `json:"sources"` // human labels, e.g. "Scanner BUY", "Promoter buy"
}
