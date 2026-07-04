// Package scanner evaluates the Pine "Chase Momentum" strategy and the four
// weekly Chartink scanners over candle series. All logic here is pure: it takes
// candles in, returns signal results out — no DB, no network.
package scanner

import (
	"math"

	"tradenexus/internal/market"
)

// ohlcv splits candles into aligned float64 series for the indicator functions.
type ohlcv struct {
	open, high, low, close, volume []float64
	n                              int
}

func toSeries(c []market.Candle) ohlcv {
	s := ohlcv{n: len(c)}
	s.open = make([]float64, len(c))
	s.high = make([]float64, len(c))
	s.low = make([]float64, len(c))
	s.close = make([]float64, len(c))
	s.volume = make([]float64, len(c))
	for i, k := range c {
		s.open[i] = k.Open
		s.high[i] = k.High
		s.low[i] = k.Low
		s.close[i] = k.Close
		s.volume[i] = float64(k.Volume)
	}
	return s
}

// shift1 returns a series where out[i] = in[i-1] (out[0] = NaN). Used to model
// Pine's `series[1]` (previous-bar) offset.
func shift1(in []float64) []float64 {
	out := make([]float64, len(in))
	if len(in) == 0 {
		return out
	}
	out[0] = math.NaN()
	for i := 1; i < len(in); i++ {
		out[i] = in[i-1]
	}
	return out
}

// maxPrev returns the max of v[i-look .. i-1] (the `look` bars before i),
// or NaN if there isn't enough history.
func maxPrev(v []float64, i, look int) float64 {
	if i-look < 0 {
		return math.NaN()
	}
	m := math.Inf(-1)
	for j := i - look; j < i; j++ {
		if v[j] > m {
			m = v[j]
		}
	}
	return m
}

func nan(vals ...float64) bool {
	for _, v := range vals {
		if math.IsNaN(v) {
			return true
		}
	}
	return false
}
