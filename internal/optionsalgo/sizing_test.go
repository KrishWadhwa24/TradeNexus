package optionsalgo

import "testing"

func TestPositionSize_RealWorldExample(t *testing.T) {
	// Confirmed against real live data (session verification): at 1,00,000
	// capital / 1% risk / 20% stop, a typical near-ATM NIFTY premium (~120)
	// with lot size 65 sizes to 0 — capital too small. At 2,50,000 it should
	// clear one lot.
	got := PositionSize(100000, 1, 119.6, 20, 65)
	if got != 0 {
		t.Errorf("at 1L capital, got %d, want 0 (undersized, matches live finding)", got)
	}
	got = PositionSize(250000, 1, 119.6, 20, 65)
	if got != 65 {
		t.Errorf("at 2.5L capital, got %d, want 65 (exactly one lot)", got)
	}
}

func TestPositionSize_ZeroCapitalOrPrice(t *testing.T) {
	if got := PositionSize(0, 1, 100, 20, 65); got != 0 {
		t.Errorf("zero capital: got %d, want 0", got)
	}
	if got := PositionSize(250000, 1, 0, 20, 65); got != 0 {
		t.Errorf("zero entry price: got %d, want 0", got)
	}
	if got := PositionSize(250000, 1, 100, 0, 65); got != 0 {
		t.Errorf("zero stop-loss percent: got %d, want 0 (avoid divide-by-zero)", got)
	}
}

func TestPositionSize_InvalidLotSize(t *testing.T) {
	if got := PositionSize(250000, 1, 100, 20, 0); got != 0 {
		t.Errorf("zero lot size: got %d, want 0", got)
	}
}

func TestPositionSize_HigherRiskSizesUp(t *testing.T) {
	low := PositionSize(100000, 1, 119.6, 20, 65)
	high := PositionSize(100000, 3, 119.6, 20, 65)
	if high <= low {
		t.Errorf("3%% risk (%d) should size to more lots than 1%% risk (%d)", high, low)
	}
}
