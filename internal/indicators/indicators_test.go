package indicators

import (
	"math"
	"testing"
)

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestSMA(t *testing.T) {
	v := []float64{1, 2, 3, 4, 5}
	out := SMA(v, 3)
	if !math.IsNaN(out[0]) || !math.IsNaN(out[1]) {
		t.Fatal("expected NaN warmup for first two")
	}
	if !approx(out[2], 2) || !approx(out[3], 3) || !approx(out[4], 4) {
		t.Fatalf("SMA wrong: %v", out)
	}
}

func TestEMA_SeedIsSMA(t *testing.T) {
	v := []float64{1, 2, 3, 4, 5}
	out := EMA(v, 3)
	if !approx(out[2], 2) { // seed = SMA of first 3 = 2
		t.Fatalf("EMA seed = %v, want 2", out[2])
	}
	// k = 2/4 = 0.5; out[3] = 4*0.5 + 2*0.5 = 3
	if !approx(out[3], 3) {
		t.Fatalf("EMA[3] = %v, want 3", out[3])
	}
	// out[4] = 5*0.5 + 3*0.5 = 4
	if !approx(out[4], 4) {
		t.Fatalf("EMA[4] = %v, want 4", out[4])
	}
}

func TestRSI_AllGainsIs100(t *testing.T) {
	v := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	out := RSI(v, 14)
	if math.IsNaN(out[14]) {
		t.Fatal("expected RSI defined at index 14")
	}
	if !approx(out[14], 100) {
		t.Fatalf("RSI of monotonic increase = %v, want 100", out[14])
	}
}

func TestHighestLowest(t *testing.T) {
	v := []float64{3, 1, 4, 1, 5, 9, 2, 6}
	hi := HighestN(v, 3)
	lo := LowestN(v, 3)
	if !approx(hi[2], 4) || !approx(hi[5], 9) {
		t.Fatalf("HighestN wrong: %v", hi)
	}
	if !approx(lo[2], 1) || !approx(lo[6], 2) {
		t.Fatalf("LowestN wrong: %v", lo)
	}
}

func TestCrossOverUnder(t *testing.T) {
	a := []float64{1, 2, 3}
	b := []float64{2, 2, 2}
	// a goes 1->2->3 vs b=2: crossover at i=2 (a[1]=2<=2, a[2]=3>2)
	if !CrossOver(a, b, 2) {
		t.Fatal("expected crossover at i=2")
	}
	if CrossOver(a, b, 1) {
		t.Fatal("no crossover at i=1")
	}
	a2 := []float64{3, 2, 1}
	if !CrossUnder(a2, b, 2) {
		t.Fatal("expected crossunder at i=2")
	}
}
