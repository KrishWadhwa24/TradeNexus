package scanner

import (
	"math"
	"testing"
	"time"

	"tradenexus/internal/market"
)

func mkCandle(o, h, l, c float64, v int64) market.Candle {
	return market.Candle{Time: time.Now(), Open: o, High: h, Low: l, Close: c, Volume: v}
}

// craft a 6-bar weekly series engineered to satisfy scanner 4 at the last bar.
func continuationCandles() []market.Candle {
	return []market.Candle{
		mkCandle(15, 20, 10, 15, 100),
		mkCandle(15, 21, 11, 15, 100),
		mkCandle(16, 22, 12, 16, 100),
		mkCandle(16, 23, 13, 16, 100),
		mkCandle(17, 19, 9, 17, 100),
		mkCandle(14, 25, 14, 24, 200), // last bar: HH, HL, green, rising closes, vol up
	}
}

func TestWeekly4_FiresWithRSIInRange(t *testing.T) {
	candles := continuationCandles()
	w := weeklyIndicators{s: toSeries(candles), rsi: make([]float64, len(candles))}
	for i := range w.rsi {
		w.rsi[i] = math.NaN()
	}
	i := len(candles) - 1
	w.rsi[i] = 60 // in the 50..70 window

	if !weekly4(w, i) {
		t.Fatalf("weekly4 should fire on engineered continuation bar")
	}

	w.rsi[i] = 80 // overbought → out of range
	if weekly4(w, i) {
		t.Fatalf("weekly4 must not fire when RSI > 70")
	}
}

func TestWeekly4_RequiresRisingLastLow(t *testing.T) {
	candles := continuationCandles()
	candles[5].Low = 8 // now low[5] < low[4] (9), breaking the higher-low rule
	w := weeklyIndicators{s: toSeries(candles), rsi: make([]float64, len(candles))}
	for i := range w.rsi {
		w.rsi[i] = 60
	}
	if weekly4(w, len(candles)-1) {
		t.Fatalf("weekly4 must not fire when higher-low rule is violated")
	}
}

func TestScanWeekly_AlwaysReportsFourScanners(t *testing.T) {
	res := ScanWeekly(continuationCandles())
	if len(res.Details) != 4 {
		t.Fatalf("expected 4 scanner results, got %d", len(res.Details))
	}
	if res.Confidence < 0 || res.Confidence > 4 {
		t.Fatalf("confidence out of range: %d", res.Confidence)
	}
}

func TestScanPine_NoSignalOnFlatData(t *testing.T) {
	var candles []market.Candle
	for i := 0; i < 60; i++ {
		candles = append(candles, mkCandle(100, 100.5, 99.5, 100, 1000))
	}
	sig := ScanPine(candles, DefaultPineConfig())
	if sig.Buy || sig.Sell {
		t.Fatalf("flat data must not produce a signal: %+v", sig)
	}
}

func TestScanPine_ShortSeriesSafe(t *testing.T) {
	sig := ScanPine([]market.Candle{mkCandle(1, 1, 1, 1, 1)}, DefaultPineConfig())
	if sig.Buy || sig.Sell {
		t.Fatal("single-candle input should never signal")
	}
}

func TestRun_WiresAllTimeframes(t *testing.T) {
	c := continuationCandles()
	rep := Run(c, c, c, DefaultPineConfig())
	if len(rep.Weekly.Details) != 4 {
		t.Fatalf("engine should populate 4 weekly scanner results")
	}
}
