package scanner

import (
	"math"

	"tradenexus/internal/market"
)

const (
	PatternDowntrendBreakout = "pattern_downtrend_breakout"
	PatternRectangle         = "pattern_rectangle"
	PatternCupHandle         = "pattern_cup_handle"
)

type Pivot struct {
	Index  int     `json:"index"`
	Price  float64 `json:"price"`
	IsHigh bool    `json:"is_high"`
}

type PatternSignal struct {
	Buy     bool            `json:"buy"`
	Reasons map[string]bool `json:"reasons"`
}

type PatternTimeframeResult struct {
	DowntrendBreakout PatternSignal `json:"downtrend_breakout"`
	Rectangle         PatternSignal `json:"rectangle"`
	CupHandle         PatternSignal `json:"cup_handle"`
}

type PatternReport struct {
	Daily   PatternTimeframeResult `json:"daily"`
	Weekly  PatternTimeframeResult `json:"weekly"`
	Monthly PatternTimeframeResult `json:"monthly"`
}

func ScanPatternTimeframe(candles []market.Candle, includeRectangle, includeCupHandle bool) PatternTimeframeResult {
	res := PatternTimeframeResult{
		DowntrendBreakout: newPatternSignal(),
		Rectangle:         newPatternSignal(),
		CupHandle:         newPatternSignal(),
	}
	if len(candles) == 0 {
		return res
	}
	res.DowntrendBreakout = ScanDowntrendBreakout(candles)
	if includeRectangle {
		res.Rectangle = ScanRectangleConsolidation(candles)
	}
	if includeCupHandle {
		res.CupHandle = ScanCupAndHandle(candles)
	}
	return res
}

func ScanDowntrendBreakout(candles []market.Candle) PatternSignal {
	sig := newPatternSignal()
	s := toSeries(candles)
	current := s.n - 1
	if current < 1 {
		return sig
	}
	avgVol20 := avgAt(s.volume, current, 20)
	rsSlope := priceSlope(s.close, current, 20)
	highs := lastNPivots(FindPivotHighs(candles, 2, 2), 4, current, 0)
	return scanDowntrendBreakout(highs, current, s.close, s.volume, avgVol20, rsSlope)
}

func ScanRectangleConsolidation(candles []market.Candle) PatternSignal {
	sig := newPatternSignal()
	s := toSeries(candles)
	current := s.n - 1
	if current < 1 {
		return sig
	}
	avgVol20 := avgAt(s.volume, current, 20)
	start := maxInt(0, current-80)
	uppers := lastNPivots(FindPivotHighs(candles, 2, 2), 3, current, start)
	lowers := lastNPivots(FindPivotLows(candles, 2, 2), 3, current, start)
	return scanRectangleConsolidation(uppers, lowers, current, s.close, s.volume, avgVol20)
}

func ScanCupAndHandle(candles []market.Candle) PatternSignal {
	sig := newPatternSignal()
	s := toSeries(candles)
	current := s.n - 1
	if current < 1 {
		return sig
	}
	avgVol20 := avgAt(s.volume, current, 20)
	pivots := lastNPivots(FindMajorPivots(candles, 2, 2), 4, current, 0)
	return scanCupAndHandle(pivots, current, s.close, s.volume, avgVol20)
}

func scanDowntrendBreakout(highs []Pivot, current int, close, volume []float64, avgVol20, rsSlope float64) PatternSignal {
	sig := newPatternSignal()
	sig.Reasons["enough_pivot_highs"] = len(highs) >= 4
	if len(highs) < 4 || current < 0 || current >= len(close) || current >= len(volume) {
		return sig
	}
	h := highs[len(highs)-4:]
	desc := h[0].Price > h[1].Price && h[1].Price > h[2].Price && h[2].Price > h[3].Price
	sig.Reasons["lower_pivot_highs"] = desc
	if !desc || h[3].Index == h[0].Index {
		return sig
	}
	m := (h[3].Price - h[0].Price) / float64(h[3].Index-h[0].Index)
	b := h[0].Price - (m * float64(h[0].Index))
	touches := 0
	for _, p := range h {
		expected := m*float64(p.Index) + b
		if p.Price != 0 && math.Abs(p.Price-expected)/p.Price <= 0.03 {
			touches++
		}
	}
	sig.Reasons["three_trendline_touches"] = touches >= 3
	if touches < 3 {
		return sig
	}
	duration := h[3].Index - h[0].Index
	sig.Reasons["trend_duration"] = duration >= 20
	if duration < 20 {
		return sig
	}
	resistance := m*float64(current) + b
	sig.Reasons["close_breakout"] = close[current] > resistance*1.01
	sig.Reasons["volume_breakout"] = avgVol20 > 0 && volume[current] > 1.5*avgVol20
	sig.Reasons["rs_slope_positive"] = rsSlope > 0
	sig.Buy = sig.Reasons["close_breakout"] && sig.Reasons["volume_breakout"] && sig.Reasons["rs_slope_positive"]
	return sig
}

func scanRectangleConsolidation(uppers, lowers []Pivot, current int, close, volume []float64, avgVol20 float64) PatternSignal {
	sig := newPatternSignal()
	sig.Reasons["enough_upper_pivots"] = len(uppers) >= 3
	sig.Reasons["enough_lower_pivots"] = len(lowers) >= 3
	if len(uppers) < 3 || len(lowers) < 3 || current < 0 || current >= len(close) || current >= len(volume) {
		return sig
	}
	u := uppers
	l := lowers
	maxU, minU := pivotMaxMin(u)
	maxL, minL := pivotMaxMin(l)
	sig.Reasons["upper_flat"] = maxU > 0 && (maxU-minU)/maxU < 0.03
	sig.Reasons["lower_flat"] = maxL > 0 && (maxL-minL)/maxL < 0.03
	if !sig.Reasons["upper_flat"] || !sig.Reasons["lower_flat"] {
		return sig
	}
	start := minInt(u[0].Index, l[0].Index)
	duration := current - start
	sig.Reasons["duration"] = duration >= 15
	boxHeight := maxU - minL
	sig.Reasons["box_height"] = close[current] > 0 && boxHeight/close[current] >= 0.05
	first5 := avgRange(volume, start, minInt(start+5, current))
	last5 := avgRange(volume, maxInt(start, current-5), current)
	sig.Reasons["volume_contraction"] = first5 > 0 && last5 < first5
	sig.Reasons["close_breakout"] = close[current] > maxU*1.01
	sig.Reasons["volume_breakout"] = avgVol20 > 0 && volume[current] > 1.5*avgVol20
	sig.Buy = sig.Reasons["duration"] && sig.Reasons["box_height"] &&
		sig.Reasons["volume_contraction"] && sig.Reasons["close_breakout"] && sig.Reasons["volume_breakout"]
	return sig
}

func scanCupAndHandle(pivots []Pivot, current int, close, volume []float64, avgVol20 float64) PatternSignal {
	sig := newPatternSignal()
	sig.Reasons["enough_major_pivots"] = len(pivots) >= 4
	if len(pivots) < 4 || current < 0 || current >= len(close) || current >= len(volume) {
		return sig
	}

	p := pivots[len(pivots)-4:]
	shape := p[0].IsHigh && !p[1].IsHigh && p[2].IsHigh && !p[3].IsHigh
	sig.Reasons["pivot_sequence"] = shape
	if !shape {
		return sig
	}

	sig.Reasons["rim_similarity"] = p[0].Price > 0 && math.Abs(p[0].Price-p[2].Price)/p[0].Price < 0.15
	sig.Reasons["right_rim_recovered"] = p[2].Price >= 0.90*p[0].Price
	depth := (p[0].Price - p[1].Price) / p[0].Price
	sig.Reasons["cup_depth"] = depth >= 0.20 && depth <= 0.70

	leftDuration := p[1].Index - p[0].Index
	rightDuration := p[2].Index - p[1].Index
	sig.Reasons["left_duration"] = leftDuration > 3
	sig.Reasons["right_duration"] = rightDuration > 3

	longer := maxInt(leftDuration, rightDuration)
	sig.Reasons["duration_symmetry"] = longer > 0 && float64(absInt(leftDuration-rightDuration))/float64(longer) < 0.50
	sig.Reasons["handle_above_midcup"] = p[3].Price > p[1].Price+0.50*(p[0].Price-p[1].Price)

	handleDepth := (p[2].Price - p[3].Price) / p[2].Price
	sig.Reasons["handle_depth"] = handleDepth < 0.20

	handleDuration := p[3].Index - p[2].Index
	sig.Reasons["handle_duration"] = handleDuration >= 3 && handleDuration <= 15
	avgCupVolume := avgRange(volume, p[0].Index, p[2].Index+1)
	avgHandleVolume := avgRange(volume, p[2].Index, p[3].Index+1)
	sig.Reasons["handle_volume_contraction"] = avgCupVolume > 0 && avgHandleVolume < avgCupVolume
	sig.Reasons["close_breakout"] = close[current] > p[0].Price*1.01
	sig.Reasons["volume_breakout"] = avgVol20 > 0 && volume[current] > 1.5*avgVol20

	sig.Buy = sig.Reasons["pivot_sequence"] && sig.Reasons["rim_similarity"] && sig.Reasons["cup_depth"] &&
		sig.Reasons["duration_symmetry"] && sig.Reasons["handle_above_midcup"] && sig.Reasons["handle_depth"] &&
		sig.Reasons["close_breakout"] && sig.Reasons["volume_breakout"]
	return sig
}

func FindPivotHighs(candles []market.Candle, left, right int) []Pivot {
	var out []Pivot
	if left < 1 || right < 1 || len(candles) < left+right+1 {
		return out
	}
	for i := left; i < len(candles)-right; i++ {
		price := candles[i].High
		ok := true
		for j := i - left; j <= i+right; j++ {
			if j != i && candles[j].High >= price {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, Pivot{Index: i, Price: price, IsHigh: true})
		}
	}
	return out
}

func FindPivotLows(candles []market.Candle, left, right int) []Pivot {
	var out []Pivot
	if left < 1 || right < 1 || len(candles) < left+right+1 {
		return out
	}
	for i := left; i < len(candles)-right; i++ {
		price := candles[i].Low
		ok := true
		for j := i - left; j <= i+right; j++ {
			if j != i && candles[j].Low <= price {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, Pivot{Index: i, Price: price, IsHigh: false})
		}
	}
	return out
}

func FindMajorPivots(candles []market.Candle, left, right int) []Pivot {
	highs := FindPivotHighs(candles, left, right)
	lows := FindPivotLows(candles, left, right)
	all := make([]Pivot, 0, len(highs)+len(lows))
	hi, lo := 0, 0
	for hi < len(highs) || lo < len(lows) {
		if lo >= len(lows) || (hi < len(highs) && highs[hi].Index <= lows[lo].Index) {
			all = appendPivot(all, highs[hi])
			hi++
		} else {
			all = appendPivot(all, lows[lo])
			lo++
		}
	}
	return all
}

func appendPivot(in []Pivot, p Pivot) []Pivot {
	if len(in) == 0 || in[len(in)-1].IsHigh != p.IsHigh {
		return append(in, p)
	}
	last := &in[len(in)-1]
	if p.IsHigh && p.Price > last.Price {
		*last = p
	}
	if !p.IsHigh && p.Price < last.Price {
		*last = p
	}
	return in
}

func newPatternSignal() PatternSignal {
	return PatternSignal{Reasons: map[string]bool{}}
}

func recentPivots(pivots []Pivot, current, start int) []Pivot {
	out := make([]Pivot, 0, len(pivots))
	for _, p := range pivots {
		if p.Index >= start && p.Index < current {
			out = append(out, p)
		}
	}
	return out
}

func lastNPivots(pivots []Pivot, n, current, start int) []Pivot {
	filtered := recentPivots(pivots, current, start)
	if len(filtered) <= n {
		return filtered
	}
	return filtered[len(filtered)-n:]
}

func avgAt(v []float64, i, look int) float64 {
	if i-look+1 < 0 || i >= len(v) {
		return math.NaN()
	}
	return avgRange(v, i-look+1, i+1)
}

func avgRange(v []float64, start, end int) float64 {
	if start < 0 {
		start = 0
	}
	if end > len(v) {
		end = len(v)
	}
	if start >= end {
		return 0
	}
	var sum float64
	for i := start; i < end; i++ {
		sum += v[i]
	}
	return sum / float64(end-start)
}

func priceSlope(close []float64, i, look int) float64 {
	if i-look < 0 {
		return math.NaN()
	}
	return close[i] - close[i-look]
}

func pivotMaxMin(pivots []Pivot) (float64, float64) {
	maxV := math.Inf(-1)
	minV := math.Inf(1)
	for _, p := range pivots {
		if p.Price > maxV {
			maxV = p.Price
		}
		if p.Price < minV {
			minV = p.Price
		}
	}
	return maxV, minV
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
