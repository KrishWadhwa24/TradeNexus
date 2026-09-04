package optionsalgo

import "sort"

// Delta band + target, spread, and volume thresholds — exact values from the
// script's config. deltaTarget is the ideal; deltaMin/deltaMax bound the
// acceptable band (checked against |delta| so the same band works for CE's
// positive delta and PE's negative delta).
const (
	deltaTarget = 0.60
	deltaMin    = 0.55
	deltaMax    = 0.70

	strikesEachSide = 5

	maxSpreadPercent    = 1.0
	minVolumeMultiplier = 1.2
)

// OptionQuote is one contract's live snapshot — quote (LTP/bid/ask/volume/OI)
// merged with its Greeks, keyed to the instrument that priced it.
type OptionQuote struct {
	InstrumentID  int64
	Token         string
	TradingSymbol string
	StrikePrice   float64
	OptionType    string // "CE" | "PE"
	LotSize       int
	LTP           float64
	Bid           float64
	Ask           float64
	Volume        int64
	OpenInterest  float64
	Delta         float64
	Gamma         float64
	Theta         float64
	Vega          float64
	IV            float64
}

// SpreadPercent is (ask-bid)/mid*100 — 0 if bid/ask are both non-positive
// (no depth at all, e.g. an illiquid strike with an empty order book, as
// confirmed live for a deep SENSEX strike during Phase 0 verification).
func (q OptionQuote) SpreadPercent() float64 {
	if q.Bid <= 0 || q.Ask <= 0 {
		return 0
	}
	mid := (q.Bid + q.Ask) / 2
	if mid <= 0 {
		return 0
	}
	return (q.Ask - q.Bid) / mid * 100
}

// NearestATMStrikes picks the strike from `strikes` closest to spot, then
// returns up to `each` strikes on either side of it by list position — the
// script's "ATM - 5 strikes through ATM + 5 strikes". Working off the
// actual available strikes (not spot/interval rounding) means this never
// has to know or assume NIFTY's current strike interval, which isn't stored
// anywhere and has changed for other indices before.
func NearestATMStrikes(strikes []float64, spot float64, each int) []float64 {
	if len(strikes) == 0 {
		return nil
	}
	sorted := append([]float64(nil), strikes...)
	sort.Float64s(sorted)

	atmIdx := 0
	best := abs(sorted[0] - spot)
	for i, s := range sorted {
		if d := abs(s - spot); d < best {
			best = d
			atmIdx = i
		}
	}
	lo := atmIdx - each
	if lo < 0 {
		lo = 0
	}
	hi := atmIdx + each + 1
	if hi > len(sorted) {
		hi = len(sorted)
	}
	return sorted[lo:hi]
}

// SelectByDelta filters candidates whose |delta| falls in [deltaMin,
// deltaMax] and returns the one closest to deltaTarget. ok is false if no
// candidate qualifies (e.g. the whole chain is too far in- or out-of-the-money).
func SelectByDelta(candidates []OptionQuote) (OptionQuote, bool) {
	var best OptionQuote
	bestDist := -1.0
	for _, c := range candidates {
		d := abs(c.Delta)
		if d < deltaMin || d > deltaMax {
			continue
		}
		dist := abs(d - deltaTarget)
		if bestDist < 0 || dist < bestDist {
			best = c
			bestDist = dist
		}
	}
	return best, bestDist >= 0
}

// LiquidityCheck applies the script's spread/volume filter. avgVolume is the
// average current volume across the chain's candidate strikes (a
// cross-sectional peer comparison — per-contract historical intraday volume
// isn't tracked yet, so "average" here means "average across today's chain,"
// not "average over past sessions"; revisit if a time-series comparison
// turns out to matter more).
func LiquidityCheck(q OptionQuote, avgVolume float64) (ok bool, reason string) {
	if spread := q.SpreadPercent(); spread > maxSpreadPercent {
		return false, "spread too wide"
	}
	if avgVolume > 0 && float64(q.Volume) < avgVolume*minVolumeMultiplier {
		return false, "volume below liquidity threshold"
	}
	return true, ""
}

// AverageVolume is the plain mean volume across a set of quotes — the
// "average_volume" LiquidityCheck compares a single contract's volume
// against.
func AverageVolume(quotes []OptionQuote) float64 {
	if len(quotes) == 0 {
		return 0
	}
	var sum int64
	for _, q := range quotes {
		sum += q.Volume
	}
	return float64(sum) / float64(len(quotes))
}
