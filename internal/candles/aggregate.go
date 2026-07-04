// Package candles stores daily candles and derives weekly/monthly candles.
// The aggregation functions here are pure and deterministic — the easiest and
// most valuable thing to unit-test in the whole system.
package candles

import (
	"fmt"
	"sort"

	"tradenexus/internal/market"
)

// RequiredDailyBars is the default history depth to fetch when a stock is first
// added. It's sized to the deepest lookback across all strategies:
//   - weekly EMA(200)  ≈ 200 weeks  ≈ 1000 daily bars
//   - monthly EMA(50)  ≈ 50 months  ≈ 1050 daily bars
// plus warmup buffer. This stays under Angel's 2000-candle single-request cap.
const RequiredDailyBars = 1300

// Weekly aggregates daily candles into ISO-week candles.
func Weekly(daily []market.Candle) []market.AggCandle {
	return aggregate(daily, func(c market.Candle) string {
		y, w := c.Time.In(market.IST).ISOWeek()
		return fmt.Sprintf("%04d-W%02d", y, w)
	})
}

// Monthly aggregates daily candles into calendar-month candles.
func Monthly(daily []market.Candle) []market.AggCandle {
	return aggregate(daily, func(c market.Candle) string {
		t := c.Time.In(market.IST)
		return fmt.Sprintf("%04d-%02d", t.Year(), int(t.Month()))
	})
}

// aggregate groups consecutive daily candles by key() and rolls up OHLCV.
// open = first day's open, close = last day's close, high/low = extremes,
// volume = sum. Every group is confirmed except the most recent one, which is
// still forming (weekly/monthly scanners are allowed to run on it anyway).
func aggregate(daily []market.Candle, key func(market.Candle) string) []market.AggCandle {
	if len(daily) == 0 {
		return nil
	}
	// Defensive copy + chronological sort so callers needn't pre-sort.
	src := make([]market.Candle, len(daily))
	copy(src, daily)
	sort.Slice(src, func(i, j int) bool { return src[i].Time.Before(src[j].Time) })

	var out []market.AggCandle
	curKey := ""
	for _, c := range src {
		k := key(c)
		if k != curKey {
			out = append(out, market.AggCandle{
				PeriodStart: c.Time,
				PeriodEnd:   c.Time,
				Open:        c.Open,
				High:        c.High,
				Low:         c.Low,
				Close:       c.Close,
				Volume:      c.Volume,
			})
			curKey = k
			continue
		}
		g := &out[len(out)-1]
		if c.High > g.High {
			g.High = c.High
		}
		if c.Low < g.Low {
			g.Low = c.Low
		}
		g.Close = c.Close
		g.PeriodEnd = c.Time
		g.Volume += c.Volume
	}

	// All but the last group are confirmed; the last is the forming period.
	for i := range out {
		out[i].IsConfirmed = i < len(out)-1
	}
	return out
}
