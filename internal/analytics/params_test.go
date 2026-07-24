package analytics

import (
	"testing"
	"time"

	"tradenexus/internal/market"
)

func mkCandle(d int, close float64, vol int64) market.Candle {
	return market.Candle{
		Time:   time.Date(2026, 1, d%28+1, 0, 0, 0, 0, market.IST),
		Open:   close, High: close + 1, Low: close - 1, Close: close, Volume: vol,
	}
}

func TestComputeParams_Basic(t *testing.T) {
	var daily []market.Candle
	for i := 0; i < 60; i++ {
		daily = append(daily, mkCandle(i, 100+float64(i), 1000)) // steadily rising
	}
	p := ComputeParams(daily)

	if p.LastClose != daily[len(daily)-1].Close {
		t.Errorf("LastClose = %v, want %v", p.LastClose, daily[len(daily)-1].Close)
	}
	if p.PrevClose != daily[len(daily)-2].Close {
		t.Errorf("PrevClose = %v", p.PrevClose)
	}
	if p.PctChange <= 0 {
		t.Errorf("rising series should have positive pct change, got %v", p.PctChange)
	}
	if p.Volume != 1000 {
		t.Errorf("Volume = %d, want 1000", p.Volume)
	}
	// With 60 rising bars, EMAs and RSI must be defined (non-zero) and RSI high.
	if p.EMA20 == 0 || p.EMA50 == 0 || p.RSI14 == 0 {
		t.Errorf("indicators should be defined: %+v", p)
	}
	if p.RSI14 < 90 {
		t.Errorf("monotonic uptrend RSI should be very high, got %v", p.RSI14)
	}
}

func TestComputeParams_Empty(t *testing.T) {
	p := ComputeParams(nil)
	if p.LastClose != 0 || p.RSI14 != 0 {
		t.Errorf("empty input should yield zero params, got %+v", p)
	}
}
