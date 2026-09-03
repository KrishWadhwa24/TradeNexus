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

func TestSameISTDate(t *testing.T) {
	cases := []struct {
		name string
		a, b time.Time
		want bool
	}{
		{
			name: "same instant",
			a:    time.Date(2026, 8, 31, 10, 0, 0, 0, IST),
			b:    time.Date(2026, 8, 31, 10, 0, 0, 0, IST),
			want: true,
		},
		{
			name: "same IST calendar day, different times",
			a:    time.Date(2026, 8, 31, 9, 15, 0, 0, IST),
			b:    time.Date(2026, 8, 31, 15, 30, 0, 0, IST),
			want: true,
		},
		{
			name: "different days",
			a:    time.Date(2026, 8, 30, 23, 0, 0, 0, IST),
			b:    time.Date(2026, 8, 31, 1, 0, 0, 0, IST),
			want: false,
		},
		{
			name: "UTC midnight crosses into the next IST day — the whole point of comparing in IST, not UTC",
			// 2026-08-31 19:00 UTC = 2026-09-01 00:30 IST (UTC+5:30).
			a:    time.Date(2026, 8, 31, 19, 0, 0, 0, time.UTC),
			b:    time.Date(2026, 8, 31, 20, 0, 0, 0, time.UTC), // = 2026-09-01 01:30 IST
			want: true,                                          // both are Sep 1 in IST, despite being Aug 31 in UTC
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SameISTDate(c.a, c.b); got != c.want {
				t.Errorf("SameISTDate(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

func TestIST(t *testing.T) {
	// IST must be UTC+5:30.
	_, offset := time.Date(2026, 1, 1, 0, 0, 0, 0, IST).Zone()
	if offset != 5*3600+30*60 {
		t.Errorf("IST offset = %d seconds, want 19800", offset)
	}
}
