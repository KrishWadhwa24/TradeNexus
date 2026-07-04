package scanner

import (
	"tradenexus/internal/indicators"
	"tradenexus/internal/market"
)

// PineConfig holds the tunable inputs from the Pine script.
type PineConfig struct {
	BreakoutLength   int
	VolumeMultiplier float64
	CooldownBars     int
}

// DefaultPineConfig mirrors the script's defaults.
func DefaultPineConfig() PineConfig {
	return PineConfig{BreakoutLength: 20, VolumeMultiplier: 1.8, CooldownBars: 12}
}

// PineSignal is the outcome on the most recent bar.
type PineSignal struct {
	Buy     bool            `json:"buy"`
	Sell    bool            `json:"sell"`
	Reasons map[string]bool `json:"reasons"`
}

// ScanPine faithfully replays the "Chase Momentum Pro Clean" strategy over the
// candle series (the state machine + cooldown must be replayed from the start to
// be correct) and reports whether a Buy/Sell fires on the LAST bar.
func ScanPine(candles []market.Candle, cfg PineConfig) PineSignal {
	s := toSeries(candles)
	if s.n < 2 {
		return PineSignal{Reasons: map[string]bool{}}
	}

	ema10 := indicators.EMA(s.close, 10)
	ema20 := indicators.EMA(s.close, 20)
	sma40 := indicators.SMA(s.close, 40)
	rsi := indicators.RSI(s.close, 14)
	atr := indicators.ATR(s.high, s.low, s.close, 14)
	avgVol := indicators.SMA(s.volume, 20)

	// Breakout levels use the previous-bar value of highest(high,20)/lowest(low,20).
	highLevel := shift1(indicators.HighestN(s.high, cfg.BreakoutLength))
	lowLevel := shift1(indicators.LowestN(s.low, cfg.BreakoutLength))

	longActive, shortActive := false, false
	lastBuyBar, lastSellBar := -100, -100
	last := PineSignal{Reasons: map[string]bool{}}

	for i := 1; i < s.n; i++ { // i>=1: every condition references a previous bar
		prev := i - 1

		bullTrend := !nan(ema10[i], ema20[i], sma40[i], sma40[prev]) &&
			ema10[i] > ema20[i] && ema20[i] > sma40[i] &&
			s.close[i] > ema10[i] && sma40[i] > sma40[prev]
		bearTrend := !nan(ema10[i], ema20[i], sma40[i], sma40[prev]) &&
			ema10[i] < ema20[i] && ema20[i] < sma40[i] &&
			s.close[i] < ema10[i] && sma40[i] < sma40[prev]

		freshBull := indicators.CrossOver(s.close, highLevel, i)
		freshBear := indicators.CrossUnder(s.close, lowLevel, i)

		volSpike := !nan(avgVol[i]) && s.volume[i] > avgVol[i]*cfg.VolumeMultiplier

		body := absf(s.close[i] - s.open[i])
		strongBull := !nan(atr[i]) && s.close[i] > s.open[i] && body > atr[i]*0.5
		strongBear := !nan(atr[i]) && s.close[i] < s.open[i] && body > atr[i]*0.5

		bullMom := !nan(rsi[i]) && rsi[i] > 60
		bearMom := !nan(rsi[i]) && rsi[i] < 40

		// Reset state (order matches the script: resets run before signal calc).
		if !nan(ema10[i]) {
			if s.close[i] < ema10[i] || indicators.CrossUnder(ema10, ema20, i) {
				longActive = false
			}
			if s.close[i] > ema10[i] || indicators.CrossOver(ema10, ema20, i) {
				shortActive = false
			}
		}

		canBuy := (i - lastBuyBar) > cfg.CooldownBars
		canSell := (i - lastSellBar) > cfg.CooldownBars

		buy := bullTrend && freshBull && volSpike && strongBull && bullMom && !longActive && canBuy
		sell := bearTrend && freshBear && volSpike && strongBear && bearMom && !shortActive && canSell

		if buy {
			longActive, shortActive = true, false
			lastBuyBar = i
		}
		if sell {
			shortActive, longActive = true, false
			lastSellBar = i
		}

		if i == s.n-1 {
			last = PineSignal{
				Buy:  buy,
				Sell: sell,
				Reasons: map[string]bool{
					"bullTrend":         bullTrend,
					"freshBullBreakout": freshBull,
					"volumeSpike":       volSpike,
					"strongBullCandle":  strongBull,
					"bullMomentum":      bullMom,
					"canBuy":            canBuy,
				},
			}
		}
	}
	return last
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
