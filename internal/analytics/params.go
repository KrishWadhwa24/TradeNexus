package analytics

import (
	"math"

	"tradenexus/internal/indicators"
	"tradenexus/internal/market"
)

// Params holds the latest computed technical parameters for a stock (daily TF).
type Params struct {
	InstrumentID int64   `json:"instrument_id"`
	Symbol       string  `json:"symbol"`
	Price        float64 `json:"price"` // live LTP if available, else last close
	LastClose    float64 `json:"last_close"`
	PrevClose    float64 `json:"prev_close"`
	PctChange    float64 `json:"pct_change"`
	Volume       int64   `json:"volume"`
	EMA10        float64 `json:"ema10"`
	EMA20        float64 `json:"ema20"`
	EMA50        float64 `json:"ema50"`
	SMA40        float64 `json:"sma40"`
	RSI14        float64 `json:"rsi14"`
	ATR14        float64 `json:"atr14"`
	VolSMA20     float64 `json:"vol_sma20"`
}

// ComputeParams derives the latest daily indicator values from daily candles.
// price defaults to the last close; the caller may override with a live LTP.
func ComputeParams(daily []market.Candle) Params {
	var p Params
	n := len(daily)
	if n == 0 {
		return p
	}
	closes := make([]float64, n)
	highs := make([]float64, n)
	lows := make([]float64, n)
	vols := make([]float64, n)
	for i, c := range daily {
		closes[i] = c.Close
		highs[i] = c.High
		lows[i] = c.Low
		vols[i] = float64(c.Volume)
	}
	last := n - 1
	p.LastClose = closes[last]
	p.Price = closes[last]
	p.Volume = daily[last].Volume
	if n >= 2 {
		p.PrevClose = closes[last-1]
		if p.PrevClose > 0 {
			p.PctChange = (p.LastClose - p.PrevClose) / p.PrevClose * 100
		}
	}
	p.EMA10 = at(indicators.EMA(closes, 10), last)
	p.EMA20 = at(indicators.EMA(closes, 20), last)
	p.EMA50 = at(indicators.EMA(closes, 50), last)
	p.SMA40 = at(indicators.SMA(closes, 40), last)
	p.RSI14 = at(indicators.RSI(closes, 14), last)
	p.ATR14 = at(indicators.ATR(highs, lows, closes, 14), last)
	p.VolSMA20 = at(indicators.SMA(vols, 20), last)
	return p
}

// at returns s[i] or 0 when the value is undefined (NaN).
func at(s []float64, i int) float64 {
	if i < 0 || i >= len(s) || math.IsNaN(s[i]) {
		return 0
	}
	return s[i]
}
