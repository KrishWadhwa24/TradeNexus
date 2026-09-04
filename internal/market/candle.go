// Package market holds shared domain types (candles, timeframes) used across
// the Angel client, the candle store, and later the indicator/scanner engines.
// Keeping them here avoids import cycles between those packages.
package market

import "time"

// IST is the exchange timezone. All candle dates are normalized to it.
var IST = time.FixedZone("IST", 5*3600+30*60)

// SameISTDate reports whether two instants fall on the same calendar day in IST.
func SameISTDate(a, b time.Time) bool {
	a, b = a.In(IST), b.In(IST)
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

// EffectivePrice prefers the live bid-ask midpoint over a last-traded-price
// (LTP) field when a valid quote exists, falling back to LTP only when
// there's no live two-sided quote at all (an empty order book). LTP is
// mechanically a lagging number — it only ever reports a trade that already
// happened, and for a contract with thin/no recent trading (confirmed live:
// a deep-in-the-money NIFTY option showed LTP far outside its own live bid/
// ask), that trade can be stale while the live quote has moved on. Shared
// across internal/angel, internal/paper, and internal/optionsalgo — the one
// place this "what's the real current price" decision is made, rather than
// duplicated per package.
func EffectivePrice(ltp, bid, ask float64) float64 {
	if bid > 0 && ask > 0 && bid <= ask {
		return (bid + ask) / 2
	}
	return ltp
}

// Timeframe identifiers.
const (
	TF1D = "1D"
	TF1W = "1W"
	TF1M = "1M"
)

// Candle is a single OHLCV bar. For daily candles, Time is the trading date
// (midnight IST).
type Candle struct {
	Time   time.Time `json:"time"`
	Open   float64   `json:"open"`
	High   float64   `json:"high"`
	Low    float64   `json:"low"`
	Close  float64   `json:"close"`
	Volume int64     `json:"volume"`
}

// AggToCandles converts higher-timeframe aggregate bars back into plain Candles
// (keyed on PeriodStart) so the indicator/scanner code can treat every
// timeframe uniformly.
func AggToCandles(agg []AggCandle) []Candle {
	out := make([]Candle, len(agg))
	for i, a := range agg {
		out[i] = Candle{
			Time:   a.PeriodStart,
			Open:   a.Open,
			High:   a.High,
			Low:    a.Low,
			Close:  a.Close,
			Volume: a.Volume,
		}
	}
	return out
}

// AggCandle is a higher-timeframe bar (weekly/monthly) derived from daily bars.
// IsConfirmed is false for the most recent (still-forming) period.
type AggCandle struct {
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	Open        float64   `json:"open"`
	High        float64   `json:"high"`
	Low         float64   `json:"low"`
	Close       float64   `json:"close"`
	Volume      int64     `json:"volume"`
	IsConfirmed bool      `json:"is_confirmed"`
}
