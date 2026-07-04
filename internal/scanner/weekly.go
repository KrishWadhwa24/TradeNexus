package scanner

import (
	"tradenexus/internal/indicators"
	"tradenexus/internal/market"
)

// WeeklyResult aggregates the four weekly scanners for the latest weekly bar.
// Confidence is the count that fired (0..4); a signal is valid when >= 1.
type WeeklyResult struct {
	Confidence int             `json:"confidence"` // N of 4
	Fired      []string        `json:"fired"`      // names of firing scanners
	Details    map[string]bool `json:"details"`    // every scanner's result
}

// weeklyIndicators holds the series shared by the four scanners.
type weeklyIndicators struct {
	s      ohlcv
	ema20c []float64
	ema50c []float64
	ema200 []float64
	ema20v []float64
	rsi    []float64
}

func buildWeekly(candles []market.Candle) weeklyIndicators {
	s := toSeries(candles)
	return weeklyIndicators{
		s:      s,
		ema20c: indicators.EMA(s.close, 20),
		ema50c: indicators.EMA(s.close, 50),
		ema200: indicators.EMA(s.close, 200),
		ema20v: indicators.EMA(s.volume, 20),
		rsi:    indicators.RSI(s.close, 14),
	}
}

// ScanWeekly runs all four weekly scanners on the LAST weekly bar.
func ScanWeekly(candles []market.Candle) WeeklyResult {
	res := WeeklyResult{Details: map[string]bool{}}
	w := buildWeekly(candles)
	i := w.s.n - 1
	if i < 1 {
		return res
	}
	checks := []struct {
		name string
		fn   func(weeklyIndicators, int) bool
	}{
		{"weekly_1", weekly1},
		{"weekly_2", weekly2},
		{"weekly_3", weekly3},
		{"weekly_4", weekly4},
	}
	for _, c := range checks {
		ok := c.fn(w, i)
		res.Details[c.name] = ok
		if ok {
			res.Confidence++
			res.Fired = append(res.Fired, c.name)
		}
	}
	return res
}

// Scanner 1 — Weekly breakout (52-wk close high + full EMA stack + participation).
func weekly1(w weeklyIndicators, i int) bool {
	s := w.s
	hi52 := maxPrev(s.close, i, 52)
	if nan(hi52, w.ema20c[i], w.ema50c[i], w.ema200[i], w.ema20v[i], w.rsi[i]) {
		return false
	}
	return s.close[i] > hi52 &&
		s.volume[i] > w.ema20v[i] &&
		s.close[i] > w.ema20c[i] &&
		w.ema20c[i] > w.ema50c[i] && w.ema50c[i] > w.ema200[i] &&
		w.rsi[i] > 50 && w.rsi[i] < 75 &&
		s.close[i] >= s.open[i]
}

// Scanner 2 — Weekly continuation (higher low + inside-bar break, EMA stack).
func weekly2(w weeklyIndicators, i int) bool {
	s := w.s
	if nan(w.ema20c[i], w.ema50c[i], w.ema200[i], w.rsi[i]) {
		return false
	}
	return s.close[i] > s.close[i-1] &&
		s.close[i] > w.ema20c[i] &&
		w.ema20c[i] > w.ema50c[i] && w.ema50c[i] > w.ema200[i] &&
		s.low[i] >= s.low[i-1] &&
		s.close[i] > s.high[i-1] &&
		s.volume[i] >= s.volume[i-1] &&
		w.rsi[i] > 50 && w.rsi[i] < 70
}

// Scanner 3 — 52-wk high breakout, structure-based (no EMA stack).
func weekly3(w weeklyIndicators, i int) bool {
	s := w.s
	if i-52 < 0 || i-4 < 0 || nan(w.rsi[i]) {
		return false
	}
	hi52 := maxPrev(s.high, i, 52)
	vol4 := maxPrev(s.volume, i, 4)
	return s.close[i] > hi52 &&
		s.volume[i] > vol4 &&
		s.close[i] >= s.open[i] &&
		s.high[i] > s.high[i-1] &&
		s.low[i] > s.low[i-4] &&
		w.rsi[i] > 50 && w.rsi[i] < 75
}

// Scanner 4 — Pure price-action continuation (three rising closes, no EMA).
func weekly4(w weeklyIndicators, i int) bool {
	s := w.s
	if i-4 < 0 || i-2 < 0 || nan(w.rsi[i]) {
		return false
	}
	return s.close[i] > s.high[i-1] &&
		s.low[i] >= s.low[i-1] &&
		s.high[i] > s.high[i-1] &&
		s.low[i] > s.low[i-4] &&
		s.close[i] >= s.open[i] &&
		s.volume[i] >= s.volume[i-1] &&
		w.rsi[i] > 50 && w.rsi[i] < 70 &&
		s.close[i] > s.close[i-1] &&
		s.close[i-1] > s.close[i-2]
}
