package optionsalgo

import (
	"math"
	"testing"
	"time"

	"tradenexus/internal/market"
)

func mkCandle(t time.Time, o, h, l, c float64, vol int64) market.Candle {
	return market.Candle{Time: t, Open: o, High: h, Low: l, Close: c, Volume: vol}
}

func closeEnough(a, b float64) bool { return math.Abs(a-b) < 0.01 }

// testConfig returns the script's exact default values — used wherever a
// test needs an AlgoConfig but isn't specifically testing configurability.
func testConfig() AlgoConfig {
	return AlgoConfig{
		RiskPerTradePercent: 1.0, MaxDailyLossPercent: 2.0, MaxWeeklyLossPercent: 5.0,
		InitialStopLossPercent: 20.0, BreakevenTriggerPercent: 25.0,
		TrailingTriggerPercent: 40.0, TrailingDistancePercent: 25.0,
		DeltaTarget: 0.60, DeltaMin: 0.55, DeltaMax: 0.70,
		MaxSpreadPercent: 1.0, MinVolumeMultiplier: 1.2,
		EMAFastPeriod: 20, EMASlowPeriod: 50, ATRPeriod: 14, ATRAvgSpan: 20,
		ORStartHour: 9, ORStartMin: 15, OREndHour: 9, OREndMin: 45, ORMinRangePercent: 0.15,
		MaxDistanceFromVWAPATR: 1.5, StrikesEachSide: 5, MaxTradesPerDay: 1,
	}
}

func TestAggregate15Min(t *testing.T) {
	base := time.Date(2026, 9, 4, 9, 15, 0, 0, market.IST)
	var oneMin []market.Candle
	// 09:15..09:29 -> first 15-min bucket (09:15); 09:30..09:31 -> second bucket (09:30)
	for i := 0; i < 17; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		oneMin = append(oneMin, mkCandle(ts, 100+float64(i), 101+float64(i), 99+float64(i), 100+float64(i), 10))
	}
	out := Aggregate15Min(oneMin)
	if len(out) != 2 {
		t.Fatalf("got %d buckets, want 2", len(out))
	}
	if !out[0].Time.Equal(base) {
		t.Errorf("bucket 0 time = %v, want %v", out[0].Time, base)
	}
	if out[0].Open != 100 {
		t.Errorf("bucket 0 open = %v, want 100 (first bar's open)", out[0].Open)
	}
	if out[0].Close != 114 { // bar index 14 is the last one before 09:30
		t.Errorf("bucket 0 close = %v, want 114", out[0].Close)
	}
	wantBucket1 := base.Add(15 * time.Minute)
	if !out[1].Time.Equal(wantBucket1) {
		t.Errorf("bucket 1 time = %v, want %v", out[1].Time, wantBucket1)
	}
}

func TestEMA(t *testing.T) {
	base := time.Date(2026, 9, 4, 9, 15, 0, 0, market.IST)
	closes := []float64{10, 11, 12, 13, 14, 15, 16}
	var cs []market.Candle
	for i, c := range closes {
		cs = append(cs, mkCandle(base.Add(time.Duration(i)*time.Minute), c, c, c, c, 1))
	}
	out := EMA(cs, 3)
	// seed (index 2) = SMA(10,11,12) = 11
	if !closeEnough(out[2], 11) {
		t.Errorf("EMA seed = %v, want 11", out[2])
	}
	// index 3: mult = 2/4 = 0.5 -> 13*0.5 + 11*0.5 = 12
	if !closeEnough(out[3], 12) {
		t.Errorf("EMA[3] = %v, want 12", out[3])
	}
	if out[0] != 0 || out[1] != 0 {
		t.Errorf("EMA before seed should be 0, got %v, %v", out[0], out[1])
	}
}

func TestEMA_InsufficientBars(t *testing.T) {
	base := time.Date(2026, 9, 4, 9, 15, 0, 0, market.IST)
	cs := []market.Candle{mkCandle(base, 10, 10, 10, 10, 1), mkCandle(base.Add(time.Minute), 11, 11, 11, 11, 1)}
	out := EMA(cs, 20)
	for i, v := range out {
		if v != 0 {
			t.Errorf("out[%d] = %v, want 0 (not enough bars to seed)", i, v)
		}
	}
}

func TestATR(t *testing.T) {
	base := time.Date(2026, 9, 4, 9, 15, 0, 0, market.IST)
	// Constant true range of 2 every bar (high-low=2, no gaps) -> ATR should settle at 2.
	var cs []market.Candle
	for i := 0; i < 20; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		cs = append(cs, mkCandle(ts, 100, 101, 99, 100, 1))
	}
	out := ATR(cs, 14)
	if !closeEnough(out[13], 2) {
		t.Errorf("ATR seed = %v, want 2", out[13])
	}
	if !closeEnough(out[19], 2) {
		t.Errorf("ATR[19] = %v, want 2 (should stay at 2 with constant TR)", out[19])
	}
}

func TestATRAverage(t *testing.T) {
	atr := make([]float64, 25)
	for i := range atr {
		atr[i] = float64(i + 1) // 1,2,3...
	}
	out := ATRAverage(atr, 20)
	// out[20] = avg(atr[0:20]) = avg(1..20) = 10.5
	if !closeEnough(out[20], 10.5) {
		t.Errorf("ATRAverage[20] = %v, want 10.5", out[20])
	}
	for i := 0; i < 20; i++ {
		if out[i] != 0 {
			t.Errorf("out[%d] = %v, want 0 (fewer than span prior values)", i, out[i])
		}
	}
}

func TestSessionVWAP(t *testing.T) {
	day1 := time.Date(2026, 9, 4, 9, 15, 0, 0, market.IST)
	day2 := time.Date(2026, 9, 5, 9, 15, 0, 0, market.IST)
	cs := []market.Candle{
		mkCandle(day1, 100, 102, 98, 100, 10),                   // typical=100, cumPV=1000, cumVol=10 -> vwap=100
		mkCandle(day1.Add(time.Minute), 100, 106, 100, 103, 10), // typical=103, cumPV=1000+1030=2030, cumVol=20 -> vwap=101.5
		mkCandle(day2, 200, 200, 200, 200, 5),                   // new day resets -> vwap=200
	}
	out := SessionVWAP(cs)
	if !closeEnough(out[0], 100) {
		t.Errorf("out[0] = %v, want 100", out[0])
	}
	if !closeEnough(out[1], 101.5) {
		t.Errorf("out[1] = %v, want 101.5", out[1])
	}
	if !closeEnough(out[2], 200) {
		t.Errorf("out[2] = %v, want 200 (session reset on new day)", out[2])
	}
}

func TestSessionVWAP_ZeroVolume(t *testing.T) {
	base := time.Date(2026, 9, 4, 9, 15, 0, 0, market.IST)
	cs := []market.Candle{mkCandle(base, 100, 100, 100, 100, 0)}
	out := SessionVWAP(cs)
	if out[0] != 0 {
		t.Errorf("out[0] = %v, want 0 when cumulative volume is 0 (spot index case)", out[0])
	}
}

func TestBuildOpeningRange(t *testing.T) {
	day := time.Date(2026, 9, 4, 0, 0, 0, 0, market.IST)
	mk := func(hh, mm int, h, l float64) market.Candle {
		ts := time.Date(2026, 9, 4, hh, mm, 0, 0, market.IST)
		return mkCandle(ts, h, h, l, h, 1)
	}
	cs := []market.Candle{
		mk(9, 10, 100, 99),  // before OR window - excluded
		mk(9, 15, 105, 100), // in window
		mk(9, 30, 110, 103), // in window - new high
		mk(9, 44, 104, 95),  // in window - new low
		mk(9, 45, 200, 200), // window is [09:15,09:45) - this bar excluded
		mk(9, 50, 300, 300), // after window - excluded
	}
	or := BuildOpeningRange(cs, day, testConfig())
	if or.High != 110 {
		t.Errorf("High = %v, want 110", or.High)
	}
	if or.Low != 95 {
		t.Errorf("Low = %v, want 95", or.Low)
	}
	wantPct := (110 - 95.0) / 95 * 100
	if !closeEnough(or.RangePercent, wantPct) {
		t.Errorf("RangePercent = %v, want %v", or.RangePercent, wantPct)
	}
}

func TestBuildOpeningRange_TooTight(t *testing.T) {
	day := time.Date(2026, 9, 4, 0, 0, 0, 0, market.IST)
	ts := time.Date(2026, 9, 4, 9, 20, 0, 0, market.IST)
	cs := []market.Candle{mkCandle(ts, 100, 100.05, 99.98, 100, 1)} // ~0.07% range
	or := BuildOpeningRange(cs, day, testConfig())
	if or.Valid {
		t.Errorf("expected Valid=false for a %.3f%% range (< %.2f%% minimum)", or.RangePercent, testConfig().ORMinRangePercent)
	}
}

func TestBuildOpeningRange_NoData(t *testing.T) {
	day := time.Date(2026, 9, 4, 0, 0, 0, 0, market.IST)
	or := BuildOpeningRange(nil, day, testConfig())
	if or.Valid || or.High != 0 {
		t.Errorf("expected zero-value invalid OpeningRange for no data, got %+v", or)
	}
}

func TestDetermineDirection_Bullish(t *testing.T) {
	in := DirectionInputs{
		Spot: 110, OR: OpeningRange{High: 105, Low: 100, RangePercent: 5, Valid: true},
		VWAP: 108, EMAFast: 106, EMASlow: 104, ATR: 20, ATRAvg: 15,
	}
	got := DetermineDirection(in)
	if got.Direction != Bullish {
		t.Errorf("Direction = %v, want BULLISH (reason: %s)", got.Direction, got.Reason)
	}
}

func TestDetermineDirection_Bearish(t *testing.T) {
	in := DirectionInputs{
		Spot: 90, OR: OpeningRange{High: 105, Low: 100, RangePercent: 5, Valid: true},
		VWAP: 95, EMAFast: 96, EMASlow: 99, ATR: 20, ATRAvg: 15,
	}
	got := DetermineDirection(in)
	if got.Direction != Bearish {
		t.Errorf("Direction = %v, want BEARISH (reason: %s)", got.Direction, got.Reason)
	}
}

func TestDetermineDirection_InsideRange(t *testing.T) {
	in := DirectionInputs{
		Spot: 102, OR: OpeningRange{High: 105, Low: 100, RangePercent: 5, Valid: true},
		VWAP: 102, EMAFast: 103, EMASlow: 101, ATR: 20, ATRAvg: 15,
	}
	got := DetermineDirection(in)
	if got.Direction != NoneDir {
		t.Errorf("Direction = %v, want NONE (price inside OR)", got.Direction)
	}
}

func TestDetermineDirection_ATRNotElevated(t *testing.T) {
	in := DirectionInputs{
		Spot: 110, OR: OpeningRange{High: 105, Low: 100, RangePercent: 5, Valid: true},
		VWAP: 108, EMAFast: 106, EMASlow: 104, ATR: 10, ATRAvg: 15,
	}
	got := DetermineDirection(in)
	if got.Direction != NoneDir {
		t.Errorf("Direction = %v, want NONE (ATR not elevated)", got.Direction)
	}
}

func TestDetermineDirection_EMADoesNotConfirm(t *testing.T) {
	in := DirectionInputs{
		Spot: 110, OR: OpeningRange{High: 105, Low: 100, RangePercent: 5, Valid: true},
		VWAP: 108, EMAFast: 104, EMASlow: 106, ATR: 20, ATRAvg: 15,
	}
	got := DetermineDirection(in)
	if got.Direction != NoneDir {
		t.Errorf("Direction = %v, want NONE (EMA doesn't confirm)", got.Direction)
	}
}

func TestDetermineDirection_InvalidOR(t *testing.T) {
	in := DirectionInputs{
		Spot: 110, OR: OpeningRange{High: 100.05, Low: 100, RangePercent: 0.05, Valid: false},
		VWAP: 108, EMAFast: 106, EMASlow: 104, ATR: 20, ATRAvg: 15,
	}
	got := DetermineDirection(in)
	if got.Direction != NoneDir {
		t.Errorf("Direction = %v, want NONE (invalid OR)", got.Direction)
	}
}
