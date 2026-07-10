package scanner

import (
	"testing"

	"tradenexus/internal/market"
)

func TestPatternBreakoutIndex(t *testing.T) {
	if got := patternBreakoutIndex(10, true); got != 9 {
		t.Fatalf("confirmed: want breakout index 9, got %d", got)
	}
	if got := patternBreakoutIndex(10, false); got != 8 {
		t.Fatalf("forming: want breakout index 8 (step back), got %d", got)
	}
}

// A breakout that exists ONLY on the last (forming) bar must NOT fire once the
// pattern scanner steps back to the previous, confirmed bar. Reuses the
// downtrend setup: index 40 is the breakout, index 39 is not.
func TestConfirmedBreakout_IgnoresFormingBar(t *testing.T) {
	highs := []Pivot{
		{Index: 0, Price: 100, IsHigh: true},
		{Index: 10, Price: 90, IsHigh: true},
		{Index: 20, Price: 80, IsHigh: true},
		{Index: 30, Price: 70, IsHigh: true},
	}
	close := make([]float64, 41)
	volume := make([]float64, 41)
	for i := range close {
		close[i] = 60
		volume[i] = 100
	}
	close[40] = 62 // breakout only on the final (forming) bar
	volume[40] = 180

	// Evaluated AT the breakout bar (40) → fires.
	if !scanDowntrendBreakout(highs, 40, close, volume, 2).Buy {
		t.Fatal("expected a buy when the breakout bar (40) is evaluated")
	}
	// Evaluated at the previous (confirmed) bar (39) → must NOT fire.
	if scanDowntrendBreakout(highs, 39, close, volume, 2).Buy {
		t.Fatal("must NOT fire on the confirmed bar when the breakout is only on the forming bar")
	}
}

// ScanDowntrendBreakout wrapper must pick the confirmed bar when lastConfirmed
// is false — proving forming-bar breakouts are ignored end-to-end. We assert the
// forming path never fires on a last-bar-only breakout.
func TestScanDowntrendBreakout_ConfirmedGate(t *testing.T) {
	// Build a real candle series: descending swing highs (100→70) with 2-2
	// fractal pivots, flat body until a final breakout bar.
	n := 42
	candles := make([]market.Candle, n)
	base := 60.0
	for i := 0; i < n; i++ {
		candles[i] = mkCandle(base, base+0.5, base-0.5, base, 100)
	}
	// Insert descending pivot highs at 5,15,25,35 (each a local max vs 2 neighbors).
	for _, ph := range []struct {
		idx int
		hi  float64
	}{{5, 100}, {15, 90}, {25, 80}, {35, 70}} {
		candles[ph.idx] = mkCandle(base, ph.hi, base-0.5, base, 100)
	}
	// Final bar is the breakout (last index): closes well above the trendline
	// on high volume.
	candles[n-1] = mkCandle(base, 71, base, 70, 250)

	// Confirmed → the breakout bar is evaluated → fires.
	if !ScanDowntrendBreakout(candles, true).Buy {
		t.Fatal("confirmed gate: the breakout bar should produce a signal")
	}
	// Forming → the breakout bar is excluded → must not fire.
	if ScanDowntrendBreakout(candles, false).Buy {
		t.Fatal("forming gate: a last-bar-only breakout must not produce a signal")
	}
}
