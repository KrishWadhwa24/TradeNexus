package optionsalgo

import (
	"fmt"
	"time"

	"tradenexus/internal/candles"
	"tradenexus/internal/market"
)

// Direction is the market-direction engine's output.
type Direction string

const (
	Bullish Direction = "BULLISH"
	Bearish Direction = "BEARISH"
	NoneDir Direction = "NONE"
)

// Aggregate15Min buckets 1-minute candles into 15-minute bars — reuses the
// same rollup logic already used for weekly/monthly equity candles
// (candles.Aggregate), just with a 15-minute IST bucket key instead of a
// week/month key. IST's UTC offset (+5:30 = 330 minutes) is itself an exact
// multiple of 15, so bucketing by wall-clock IST time never straddles a UTC
// storage boundary oddly.
func Aggregate15Min(oneMin []market.Candle) []market.Candle {
	agg := candles.Aggregate(oneMin, func(c market.Candle) string {
		t := c.Time.In(market.IST)
		bucketMin := (t.Minute() / 15) * 15
		return fmt.Sprintf("%04d-%02d-%02d %02d:%02d", t.Year(), t.Month(), t.Day(), t.Hour(), bucketMin)
	})
	return market.AggToCandles(agg)
}

// EMA returns the exponential moving average of closing prices, one value
// per input candle (zero for indices before the series has `period` bars to
// seed from). Standard formula: seed = SMA of the first `period` closes,
// then each subsequent value blends in the new close by the smoothing
// constant 2/(period+1).
func EMA(cs []market.Candle, period int) []float64 {
	out := make([]float64, len(cs))
	if period <= 0 || len(cs) < period {
		return out
	}
	var sum float64
	for i := 0; i < period; i++ {
		sum += cs[i].Close
	}
	seed := sum / float64(period)
	out[period-1] = seed
	mult := 2.0 / float64(period+1)
	prev := seed
	for i := period; i < len(cs); i++ {
		prev = cs[i].Close*mult + prev*(1-mult)
		out[i] = prev
	}
	return out
}

// ATR returns Wilder's Average True Range, one value per input candle (zero
// before the series has `period` bars to seed from). True Range for bar i is
// max(high-low, |high-prevClose|, |low-prevClose|); the first bar has no
// prior close, so its TR is just high-low.
func ATR(cs []market.Candle, period int) []float64 {
	out := make([]float64, len(cs))
	if period <= 0 || len(cs) == 0 {
		return out
	}
	tr := make([]float64, len(cs))
	tr[0] = cs[0].High - cs[0].Low
	for i := 1; i < len(cs); i++ {
		hl := cs[i].High - cs[i].Low
		hc := abs(cs[i].High - cs[i-1].Close)
		lc := abs(cs[i].Low - cs[i-1].Close)
		tr[i] = maxOf(hl, hc, lc)
	}
	if len(cs) < period {
		return out
	}
	var sum float64
	for i := 0; i < period; i++ {
		sum += tr[i]
	}
	prev := sum / float64(period)
	out[period-1] = prev
	for i := period; i < len(cs); i++ {
		prev = (prev*float64(period-1) + tr[i]) / float64(period)
		out[i] = prev
	}
	return out
}

// ATRAverage returns, for each index, the simple average of the `span` ATR
// values strictly before it (zero where fewer than `span` prior ATR values
// exist). This is the script's "average ATR over previous 20 periods" — a
// moving average of the ATR indicator itself, used to confirm today's ATR is
// elevated (ATR14 > ATRAverage) rather than compressed/quiet.
func ATRAverage(atr []float64, span int) []float64 {
	out := make([]float64, len(atr))
	if span <= 0 {
		return out
	}
	for i := span; i < len(atr); i++ {
		var sum float64
		for j := i - span; j < i; j++ {
			sum += atr[j]
		}
		out[i] = sum / float64(span)
	}
	return out
}

// SessionVWAP returns the cumulative volume-weighted average price for each
// bar, resetting at the start of each IST calendar day — the standard
// session VWAP. Computed on the NIFTY future's 1-minute candles (real
// volume), not the spot index (always 0 volume, confirmed live) — see the
// options-algo plan's VWAP resolution.
func SessionVWAP(oneMin []market.Candle) []float64 {
	out := make([]float64, len(oneMin))
	var cumPV, cumVol float64
	var curDay int
	for i, c := range oneMin {
		day := c.Time.In(market.IST).YearDay()
		if day != curDay {
			cumPV, cumVol = 0, 0
			curDay = day
		}
		typical := (c.High + c.Low + c.Close) / 3
		cumPV += typical * float64(c.Volume)
		cumVol += float64(c.Volume)
		if cumVol > 0 {
			out[i] = cumPV / cumVol
		}
	}
	return out
}

// OpeningRange is the result of BuildOpeningRange for one trading day.
type OpeningRange struct {
	High, Low    float64
	RangePercent float64
	Valid        bool // false if the range is too tight to be a real level (< cfg.ORMinRangePercent)
}

// BuildOpeningRange computes the opening-range high/low (window and minimum
// valid range % both from cfg, not hardcoded — script defaults are 09:15-
// 09:45 / 0.15%, frontend-editable) from one day's 1-minute spot candles.
// day identifies which calendar date (IST) to use — oneMin may span many
// days, only that day's bars are considered.
func BuildOpeningRange(oneMin []market.Candle, day time.Time, cfg AlgoConfig) OpeningRange {
	day = day.In(market.IST)
	start := time.Date(day.Year(), day.Month(), day.Day(), cfg.ORStartHour, cfg.ORStartMin, 0, 0, market.IST)
	end := time.Date(day.Year(), day.Month(), day.Day(), cfg.OREndHour, cfg.OREndMin, 0, 0, market.IST)

	var high, low float64
	found := false
	for _, c := range oneMin {
		t := c.Time.In(market.IST)
		if t.Before(start) || !t.Before(end) {
			continue
		}
		if !found {
			high, low = c.High, c.Low
			found = true
			continue
		}
		if c.High > high {
			high = c.High
		}
		if c.Low < low {
			low = c.Low
		}
	}
	if !found || low <= 0 {
		return OpeningRange{}
	}
	rangePct := (high - low) / low * 100
	return OpeningRange{High: high, Low: low, RangePercent: rangePct, Valid: rangePct >= cfg.ORMinRangePercent}
}

// DirectionInputs bundles the latest indicator readings DetermineDirection
// needs — the caller (Phase 4's execution bridge) is responsible for
// resolving these from stored/live data; this function is pure evaluation
// logic only, so it stays trivially unit-testable.
type DirectionInputs struct {
	Spot    float64
	OR      OpeningRange
	VWAP    float64
	EMAFast float64
	EMASlow float64
	ATR     float64
	ATRAvg  float64
	// MinRangePercent is cfg.ORMinRangePercent at evaluation time — carried
	// here only so DetermineDirection's "too tight" message can cite the
	// actual threshold that was used, without needing the full AlgoConfig.
	MinRangePercent float64
}

// DirectionResult carries both the classification and the values that
// produced it — the latter is what the decision log (Phase 5) records for
// every no-trade day too, per the script's explicit logging requirement.
type DirectionResult struct {
	Direction Direction
	Reason    string
}

// DetermineDirection applies the script's exact 4-condition bullish/bearish
// test. If the opening range isn't valid (too tight), or fewer conditions
// hold than required, the result is NoneDir with a reason explaining why —
// always a reason, even on no-signal, since every evaluation gets logged.
func DetermineDirection(in DirectionInputs) DirectionResult {
	if !in.OR.Valid {
		return DirectionResult{NoneDir, fmt.Sprintf("opening range too tight (%.3f%% < %.2f%% minimum)", in.OR.RangePercent, in.MinRangePercent)}
	}

	bullish := in.Spot > in.OR.High && in.Spot > in.VWAP && in.EMAFast > in.EMASlow && in.ATR > in.ATRAvg
	if bullish {
		return DirectionResult{Bullish, "NIFTY above OR high, above VWAP, EMA20>EMA50, ATR elevated"}
	}

	bearish := in.Spot < in.OR.Low && in.Spot < in.VWAP && in.EMAFast < in.EMASlow && in.ATR > in.ATRAvg
	if bearish {
		return DirectionResult{Bearish, "NIFTY below OR low, below VWAP, EMA20<EMA50, ATR elevated"}
	}

	switch {
	case in.Spot >= in.OR.Low && in.Spot <= in.OR.High:
		return DirectionResult{NoneDir, "NIFTY inside opening range"}
	case in.ATR <= in.ATRAvg:
		return DirectionResult{NoneDir, "ATR not elevated vs recent average"}
	case (in.Spot > in.OR.High && in.Spot <= in.VWAP) || (in.Spot < in.OR.Low && in.Spot >= in.VWAP):
		return DirectionResult{NoneDir, "NIFTY on the wrong side of VWAP for its OR breakout"}
	default:
		return DirectionResult{NoneDir, "EMA20/EMA50 do not confirm the breakout direction"}
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func maxOf(vals ...float64) float64 {
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
