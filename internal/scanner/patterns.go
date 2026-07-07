package scanner

import (
	"fmt"
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

type Reason struct {
	Name     string `json:"name"`
	Met      bool   `json:"met"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type PatternSignal struct {
	Buy     bool     `json:"buy"`
	Reasons []Reason `json:"reasons"`
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
	enoughPivots := len(highs) >= 3
	sig.Reasons = append(sig.Reasons, Reason{Name: "enough_pivot_highs", Met: enoughPivots, Expected: ">= 3", Actual: fmt.Sprintf("%d", len(highs))})
	if !enoughPivots {
		return sig
	}
	m, b := pivotLine(highs)
	isDowntrend := m < 0
	sig.Reasons = append(sig.Reasons, Reason{Name: "downtrend_slope", Met: isDowntrend, Expected: "< 0", Actual: fmt.Sprintf("%.2f", m)})
	if !isDowntrend {
		return sig
	}
	touches := 0
	for _, p := range highs {
		expected := m*float64(p.Index) + b
		if p.Price != 0 && math.Abs(p.Price-expected)/p.Price <= 0.05 {
			touches++
		}
	}
	threeTouches := touches >= 3
	sig.Reasons = append(sig.Reasons, Reason{Name: "three_trendline_touches", Met: threeTouches, Expected: ">= 3", Actual: fmt.Sprintf("%d", touches)})
	if !threeTouches {
		return sig
	}
	firstPivot := highs[0]
	lastPivot := highs[len(highs)-1]
	duration := lastPivot.Index - firstPivot.Index
	durationMet := duration >= 20
	sig.Reasons = append(sig.Reasons, Reason{Name: "trend_duration", Met: durationMet, Expected: ">= 20", Actual: fmt.Sprintf("%d", duration)})
	if !durationMet {
		return sig
	}
	resistance := m*float64(current) + b
	closeBreakout := close[current] > resistance*1.01
	sig.Reasons = append(sig.Reasons, Reason{Name: "close_breakout", Met: closeBreakout, Expected: fmt.Sprintf("> %.2f", resistance*1.01), Actual: fmt.Sprintf("%.2f", close[current])})
	volumeBreakout := avgVol20 > 0 && volume[current] > 1.5*avgVol20
	sig.Reasons = append(sig.Reasons, Reason{Name: "volume_breakout", Met: volumeBreakout, Expected: fmt.Sprintf("> %.2f", 1.5*avgVol20), Actual: fmt.Sprintf("%.2f", volume[current])})
	rsSlopePositive := rsSlope > 0
	sig.Reasons = append(sig.Reasons, Reason{Name: "rs_slope_positive", Met: rsSlopePositive, Expected: "> 0", Actual: fmt.Sprintf("%.2f", rsSlope)})
	sig.Buy = closeBreakout && volumeBreakout && rsSlopePositive
	return sig
}

func scanRectangleConsolidation(uppers, lowers []Pivot, current int, close, volume []float64, avgVol20 float64) PatternSignal {
	sig := newPatternSignal()
	enoughUppers := len(uppers) >= 2
	enoughLowers := len(lowers) >= 2
	sig.Reasons = append(sig.Reasons, Reason{Name: "enough_upper_pivots", Met: enoughUppers, Expected: ">= 2", Actual: fmt.Sprintf("%d", len(uppers))})
	sig.Reasons = append(sig.Reasons, Reason{Name: "enough_lower_pivots", Met: enoughLowers, Expected: ">= 2", Actual: fmt.Sprintf("%d", len(lowers))})
	if !enoughUppers || !enoughLowers {
		return sig
	}
	maxU, minU := pivotMaxMin(uppers)
	maxL, minL := pivotMaxMin(lowers)
	upperFlat := maxU > 0 && (maxU-minU)/maxU < 0.05
	lowerFlat := maxL > 0 && (maxL-minL)/maxL < 0.05
	sig.Reasons = append(sig.Reasons, Reason{Name: "upper_flat", Met: upperFlat, Expected: "< 5%", Actual: fmt.Sprintf("%.2f%%", (maxU-minU)/maxU*100)})
	sig.Reasons = append(sig.Reasons, Reason{Name: "lower_flat", Met: lowerFlat, Expected: "< 5%", Actual: fmt.Sprintf("%.2f%%", (maxL-minL)/maxL*100)})
	if !upperFlat || !lowerFlat {
		return sig
	}
	start := minInt(uppers[0].Index, lowers[0].Index)
	duration := current - start
	durationMet := duration >= 20
	sig.Reasons = append(sig.Reasons, Reason{Name: "duration", Met: durationMet, Expected: ">= 20", Actual: fmt.Sprintf("%d", duration)})
	boxHeight := maxU - minL
	boxHeightMet := close[current] > 0 && boxHeight/close[current] >= 0.05
	sig.Reasons = append(sig.Reasons, Reason{Name: "box_height", Met: boxHeightMet, Expected: ">= 5%", Actual: fmt.Sprintf("%.2f%%", boxHeight/close[current]*100)})
	firstVol := avgRange(volume, start, minInt(start+5, current))
	lastVol := avgRange(volume, maxInt(start, current-5), current)
	volumeContraction := firstVol > 0 && lastVol < firstVol
	sig.Reasons = append(sig.Reasons, Reason{Name: "volume_contraction", Met: volumeContraction, Expected: fmt.Sprintf("< %.2f", firstVol), Actual: fmt.Sprintf("%.2f", lastVol)})
	closeBreakout := close[current] > maxU*1.01
	sig.Reasons = append(sig.Reasons, Reason{Name: "close_breakout", Met: closeBreakout, Expected: fmt.Sprintf("> %.2f", maxU*1.01), Actual: fmt.Sprintf("%.2f", close[current])})
	volumeBreakout := avgVol20 > 0 && volume[current] > 1.5*avgVol20
	sig.Reasons = append(sig.Reasons, Reason{Name: "volume_breakout", Met: volumeBreakout, Expected: fmt.Sprintf("> %.2f", 1.5*avgVol20), Actual: fmt.Sprintf("%.2f", volume[current])})
	sig.Buy = durationMet && boxHeightMet && volumeContraction && closeBreakout && volumeBreakout
	return sig
}

func scanCupAndHandle(pivots []Pivot, current int, close, volume []float64, avgVol20 float64) PatternSignal {
	sig := newPatternSignal()
	enoughPivots := len(pivots) >= 4
	sig.Reasons = append(sig.Reasons, Reason{Name: "enough_major_pivots", Met: enoughPivots, Expected: ">= 4", Actual: fmt.Sprintf("%d", len(pivots))})
	if !enoughPivots {
		return sig
	}
	p := pivots[len(pivots)-4:]
	shape := p[0].IsHigh && !p[1].IsHigh && p[2].IsHigh && !p[3].IsHigh
	sig.Reasons = append(sig.Reasons, Reason{Name: "pivot_sequence", Met: shape, Expected: "H-L-H-L", Actual: getPivotSequence(p)})
	if !shape {
		return sig
	}
	rimSimilarity := p[0].Price > 0 && math.Abs(p[0].Price-p[2].Price)/p[0].Price < 0.15
	sig.Reasons = append(sig.Reasons, Reason{Name: "rim_similarity", Met: rimSimilarity, Expected: "< 15%", Actual: fmt.Sprintf("%.2f%%", math.Abs(p[0].Price-p[2].Price)/p[0].Price*100)})
	rightRimRecovered := p[2].Price >= 0.85*p[0].Price
	sig.Reasons = append(sig.Reasons, Reason{Name: "right_rim_recovered", Met: rightRimRecovered, Expected: ">= 85%", Actual: fmt.Sprintf("%.2f%%", p[2].Price/p[0].Price*100)})
	depth := (p[0].Price - p[1].Price) / p[0].Price
	cupDepth := depth >= 0.15 && depth <= 0.80
	sig.Reasons = append(sig.Reasons, Reason{Name: "cup_depth", Met: cupDepth, Expected: "15-80%", Actual: fmt.Sprintf("%.2f%%", depth*100)})
	leftDuration := p[1].Index - p[0].Index
	rightDuration := p[2].Index - p[1].Index
	leftDurationMet := leftDuration > 5
	rightDurationMet := rightDuration > 5
	sig.Reasons = append(sig.Reasons, Reason{Name: "left_duration", Met: leftDurationMet, Expected: "> 5", Actual: fmt.Sprintf("%d", leftDuration)})
	sig.Reasons = append(sig.Reasons, Reason{Name: "right_duration", Met: rightDurationMet, Expected: "> 5", Actual: fmt.Sprintf("%d", rightDuration)})
	longer := maxInt(leftDuration, rightDuration)
	durationSymmetry := longer > 0 && float64(absInt(leftDuration-rightDuration))/float64(longer) < 0.50
	sig.Reasons = append(sig.Reasons, Reason{Name: "duration_symmetry", Met: durationSymmetry, Expected: "< 50%", Actual: fmt.Sprintf("%.2f%%", float64(absInt(leftDuration-rightDuration))/float64(longer)*100)})
	handleAboveMidCup := p[3].Price > p[1].Price+0.40*(p[0].Price-p[1].Price)
	sig.Reasons = append(sig.Reasons, Reason{Name: "handle_above_midcup", Met: handleAboveMidCup, Expected: "above mid cup", Actual: fmt.Sprintf("%.2f vs %.2f", p[3].Price, p[1].Price+0.40*(p[0].Price-p[1].Price))})
	handleDepthVal := (p[2].Price - p[3].Price) / p[2].Price
	handleDepth := handleDepthVal < 0.20
	sig.Reasons = append(sig.Reasons, Reason{Name: "handle_depth", Met: handleDepth, Expected: "< 20%", Actual: fmt.Sprintf("%.2f%%", handleDepthVal*100)})
	handleDurationVal := p[3].Index - p[2].Index
	handleDuration := handleDurationVal >= 4 && handleDurationVal <= 20
	sig.Reasons = append(sig.Reasons, Reason{Name: "handle_duration", Met: handleDuration, Expected: "4-20", Actual: fmt.Sprintf("%d", handleDurationVal)})
	avgCupVolume := avgRange(volume, p[0].Index, p[2].Index+1)
	avgHandleVolume := avgRange(volume, p[2].Index, p[3].Index+1)
	handleVolumeContraction := avgCupVolume > 0 && avgHandleVolume < avgCupVolume
	sig.Reasons = append(sig.Reasons, Reason{Name: "handle_volume_contraction", Met: handleVolumeContraction, Expected: fmt.Sprintf("< %.2f", avgCupVolume), Actual: fmt.Sprintf("%.2f", avgHandleVolume)})
	closeBreakout := close[current] > p[0].Price*1.01
	sig.Reasons = append(sig.Reasons, Reason{Name: "close_breakout", Met: closeBreakout, Expected: fmt.Sprintf("> %.2f", p[0].Price*1.01), Actual: fmt.Sprintf("%.2f", close[current])})
	volumeBreakout := avgVol20 > 0 && volume[current] > 1.5*avgVol20
	sig.Reasons = append(sig.Reasons, Reason{Name: "volume_breakout", Met: volumeBreakout, Expected: fmt.Sprintf("> %.2f", 1.5*avgVol20), Actual: fmt.Sprintf("%.2f", volume[current])})

	allMet := true
	for _, r := range sig.Reasons {
		if !r.Met {
			allMet = false
			break
		}
	}
	sig.Buy = allMet
	return sig
}

func getPivotSequence(pivots []Pivot) string {
	var s string
	for i, p := range pivots {
		if i > 0 {
			s += "-"
		}
		if p.IsHigh {
			s += "H"
		} else {
			s += "L"
		}
	}
	return s
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
	return PatternSignal{Reasons: []Reason{}}
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

func pivotLine(pivots []Pivot) (float64, float64) {
	var sumX, sumY, sumXY, sumX2 float64
	n := float64(len(pivots))
	for _, p := range pivots {
		x := float64(p.Index)
		y := p.Price
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}
	if denom := n*sumX2 - sumX*sumX; denom != 0 {
		m := (n*sumXY - sumX*sumY) / denom
		b := (sumY - m*sumX) / n
		return m, b
	}
	return 0, 0
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
