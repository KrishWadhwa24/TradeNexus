package scanner

import (
	"testing"

	"tradenexus/internal/market"
)

func TestFindPivotHighsAndLows(t *testing.T) {
	candles := []market.Candle{
		mkCandle(9, 10, 8, 9, 100),
		mkCandle(11, 12, 10, 11, 100),
		mkCandle(8, 9, 7, 8, 100),
		mkCandle(13, 14, 12, 13, 100),
		mkCandle(9, 10, 8, 9, 100),
	}
	highs := FindPivotHighs(candles, 1, 1)
	lows := FindPivotLows(candles, 1, 1)
	if len(highs) != 2 || highs[0].Index != 1 || highs[1].Index != 3 {
		t.Fatalf("unexpected highs: %+v", highs)
	}
	if len(lows) != 1 || lows[0].Index != 2 {
		t.Fatalf("unexpected lows: %+v", lows)
	}
}

func TestDowntrendBreakoutAlgorithm(t *testing.T) {
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
	close[40] = 62
	volume[40] = 180
	sig := scanDowntrendBreakout(highs, 40, close, volume, 2)
	if !sig.Buy {
		t.Fatalf("expected buy, reasons=%+v", sig.Reasons)
	}

	highs[2].Price = 95
	sig = scanDowntrendBreakout(highs, 40, close, volume, 2)
	if sig.Buy || sig.Reasons["lower_pivot_highs"] {
		t.Fatalf("non-descending highs must fail: %+v", sig.Reasons)
	}
}

func TestRectangleConsolidationAlgorithm(t *testing.T) {
	uppers := []Pivot{
		{Index: 2, Price: 100, IsHigh: true},
		{Index: 10, Price: 101, IsHigh: true},
		{Index: 18, Price: 99.5, IsHigh: true},
	}
	lowers := []Pivot{
		{Index: 4, Price: 90, IsHigh: false},
		{Index: 12, Price: 91, IsHigh: false},
		{Index: 20, Price: 90.5, IsHigh: false},
	}
	close := make([]float64, 31)
	volume := make([]float64, 31)
	for i := range close {
		close[i] = 95
		volume[i] = 120
	}
	for i := 2; i < 7; i++ {
		volume[i] = 200
	}
	for i := 25; i < 30; i++ {
		volume[i] = 80
	}
	close[30] = 103
	volume[30] = 190

	sig := scanRectangleConsolidation(uppers, lowers, 30, close, volume)
	if !sig.Buy {
		t.Fatalf("expected buy, reasons=%+v", sig.Reasons)
	}

	uppers[1].Price = 106
	sig = scanRectangleConsolidation(uppers, lowers, 30, close, volume)
	if sig.Buy || sig.Reasons["upper_flat"] {
		t.Fatalf("wide upper band must fail: %+v", sig.Reasons)
	}
}

func TestCupAndHandleAlgorithm(t *testing.T) {
	pivots := []Pivot{
		{Index: 0, Price: 100, IsHigh: true},
		{Index: 8, Price: 60, IsHigh: false},
		{Index: 16, Price: 98, IsHigh: true},
		{Index: 22, Price: 90, IsHigh: false},
	}
	close := make([]float64, 31)
	volume := make([]float64, 31)
	for i := range close {
		close[i] = 80
		volume[i] = 100
	}
	for i := 0; i <= 16; i++ {
		volume[i] = 140
	}
	for i := 16; i <= 22; i++ {
		volume[i] = 80
	}
	close[30] = 102
	volume[30] = 190

	sig := scanCupAndHandle(pivots, 30, close, volume)
	if !sig.Buy {
		t.Fatalf("expected buy, reasons=%+v", sig.Reasons)
	}

	pivots[3].Price = 70
	sig = scanCupAndHandle(pivots, 30, close, volume)
	if sig.Buy || sig.Reasons["handle_depth"] {
		t.Fatalf("deep handle must fail: %+v", sig.Reasons)
	}
}
