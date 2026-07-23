package market

import (
	"testing"
	"time"
)

func TestAggToCandles(t *testing.T) {
	start := time.Date(2026, 6, 29, 0, 0, 0, 0, IST)
	agg := []AggCandle{
		{PeriodStart: start, Open: 10, High: 15, Low: 9, Close: 14, Volume: 500, IsConfirmed: true},
	}
	got := AggToCandles(agg)
	if len(got) != 1 {
		t.Fatalf("expected 1 candle, got %d", len(got))
	}
	c := got[0]
	if !c.Time.Equal(start) {
		t.Errorf("Time should equal PeriodStart, got %v", c.Time)
	}
	if c.Open != 10 || c.High != 15 || c.Low != 9 || c.Close != 14 || c.Volume != 500 {
		t.Errorf("OHLCV not copied correctly: %+v", c)
	}
}

func TestAggToCandles_Empty(t *testing.T) {
	if got := AggToCandles(nil); len(got) != 0 {
		t.Errorf("expected empty result, got %v", got)
	}
}

func TestIST(t *testing.T) {
	// IST must be UTC+5:30.
	_, offset := time.Date(2026, 1, 1, 0, 0, 0, 0, IST).Zone()
	if offset != 5*3600+30*60 {
		t.Errorf("IST offset = %d seconds, want 19800", offset)
	}
}
