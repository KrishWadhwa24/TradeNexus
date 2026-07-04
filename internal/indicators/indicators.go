// Package indicators provides pure technical-indicator functions used by the
// scanner engine. Each function returns a slice the same length as its input;
// positions that don't have enough history to be defined are math.NaN().
//
// Conventions match TradingView/Pine semantics closely enough for the scanner:
// EMA is seeded from an SMA, RSI and ATR use Wilder smoothing.
package indicators

import "math"

// SMA is the simple moving average over period p.
func SMA(v []float64, p int) []float64 {
	out := nanSlice(len(v))
	if p <= 0 || len(v) < p {
		return out
	}
	var sum float64
	for i := 0; i < len(v); i++ {
		sum += v[i]
		if i >= p {
			sum -= v[i-p]
		}
		if i >= p-1 {
			out[i] = sum / float64(p)
		}
	}
	return out
}

// EMA is the exponential moving average, seeded with the SMA of the first p
// values (defined from index p-1 onward).
func EMA(v []float64, p int) []float64 {
	out := nanSlice(len(v))
	if p <= 0 || len(v) < p {
		return out
	}
	k := 2.0 / (float64(p) + 1.0)
	var seed float64
	for i := 0; i < p; i++ {
		seed += v[i]
	}
	seed /= float64(p)
	out[p-1] = seed
	for i := p; i < len(v); i++ {
		out[i] = v[i]*k + out[i-1]*(1-k)
	}
	return out
}

// RSI is the Wilder relative strength index over period p (defined from index p).
func RSI(v []float64, p int) []float64 {
	out := nanSlice(len(v))
	if p <= 0 || len(v) <= p {
		return out
	}
	var gain, loss float64
	for i := 1; i <= p; i++ {
		ch := v[i] - v[i-1]
		if ch >= 0 {
			gain += ch
		} else {
			loss -= ch
		}
	}
	avgGain := gain / float64(p)
	avgLoss := loss / float64(p)
	out[p] = rsiFrom(avgGain, avgLoss)
	for i := p + 1; i < len(v); i++ {
		ch := v[i] - v[i-1]
		g, l := 0.0, 0.0
		if ch >= 0 {
			g = ch
		} else {
			l = -ch
		}
		avgGain = (avgGain*float64(p-1) + g) / float64(p)
		avgLoss = (avgLoss*float64(p-1) + l) / float64(p)
		out[i] = rsiFrom(avgGain, avgLoss)
	}
	return out
}

func rsiFrom(avgGain, avgLoss float64) float64 {
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

// ATR is the Wilder average true range over period p (defined from index p-1).
func ATR(high, low, close []float64, p int) []float64 {
	n := len(close)
	out := nanSlice(n)
	if p <= 0 || n < p {
		return out
	}
	tr := make([]float64, n)
	tr[0] = high[0] - low[0]
	for i := 1; i < n; i++ {
		hl := high[i] - low[i]
		hc := math.Abs(high[i] - close[i-1])
		lc := math.Abs(low[i] - close[i-1])
		tr[i] = math.Max(hl, math.Max(hc, lc))
	}
	// Seed with SMA of first p TRs (Wilder RMA seed).
	var sum float64
	for i := 0; i < p; i++ {
		sum += tr[i]
	}
	out[p-1] = sum / float64(p)
	for i := p; i < n; i++ {
		out[i] = (out[i-1]*float64(p-1) + tr[i]) / float64(p)
	}
	return out
}

// HighestN returns the rolling max over the trailing window of length p
// (inclusive of the current bar), defined from index p-1.
func HighestN(v []float64, p int) []float64 {
	out := nanSlice(len(v))
	if p <= 0 || len(v) < p {
		return out
	}
	for i := p - 1; i < len(v); i++ {
		m := v[i-p+1]
		for j := i - p + 2; j <= i; j++ {
			if v[j] > m {
				m = v[j]
			}
		}
		out[i] = m
	}
	return out
}

// LowestN returns the rolling min over the trailing window of length p.
func LowestN(v []float64, p int) []float64 {
	out := nanSlice(len(v))
	if p <= 0 || len(v) < p {
		return out
	}
	for i := p - 1; i < len(v); i++ {
		m := v[i-p+1]
		for j := i - p + 2; j <= i; j++ {
			if v[j] < m {
				m = v[j]
			}
		}
		out[i] = m
	}
	return out
}

// CrossOver reports whether series a crossed above series b at index i.
func CrossOver(a, b []float64, i int) bool {
	if i < 1 || i >= len(a) || i >= len(b) {
		return false
	}
	if isNaN(a[i], b[i], a[i-1], b[i-1]) {
		return false
	}
	return a[i] > b[i] && a[i-1] <= b[i-1]
}

// CrossUnder reports whether series a crossed below series b at index i.
func CrossUnder(a, b []float64, i int) bool {
	if i < 1 || i >= len(a) || i >= len(b) {
		return false
	}
	if isNaN(a[i], b[i], a[i-1], b[i-1]) {
		return false
	}
	return a[i] < b[i] && a[i-1] >= b[i-1]
}

// --- helpers ---

func nanSlice(n int) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = math.NaN()
	}
	return s
}

func isNaN(vals ...float64) bool {
	for _, v := range vals {
		if math.IsNaN(v) {
			return true
		}
	}
	return false
}
