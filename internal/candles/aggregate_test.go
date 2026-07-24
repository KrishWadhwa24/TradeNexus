package candles

import (
	"testing"
	"time"

	"tradenexus/internal/market"
)

func day(y int, m time.Month, d int, o, h, l, c float64, v int64) market.Candle {
	return market.Candle{
		Time: time.Date(y, m, d, 0, 0, 0, 0, market.IST),
		Open: o, High: h, Low: l, Close: c, Volume: v,
	}
}

// Two full ISO weeks of daily bars:
//
//	Week A: Mon 2024-01-01 .. Fri 2024-01-05
//	Week B: Mon 2024-01-08 .. Fri 2024-01-12
func sampleDaily() []market.Candle {
	return []market.Candle{
		day(2024, 1, 1, 100, 110, 95, 105, 1000),
		day(2024, 1, 2, 105, 112, 101, 108, 1200),
		day(2024, 1, 3, 108, 115, 104, 109, 900),
		day(2024, 1, 4, 109, 111, 100, 102, 1500),
		day(2024, 1, 5, 102, 120, 99, 118, 2000), // week A close/high
		day(2024, 1, 8, 118, 122, 116, 120, 1100),
		day(2024, 1, 9, 120, 125, 119, 124, 1300),
		day(2024, 1, 10, 124, 130, 121, 128, 1700), // week B high
		day(2024, 1, 11, 128, 129, 123, 126, 800),
		day(2024, 1, 12, 126, 127, 90, 121, 2500), // week B low/close
	}
}

func TestWeekly_OHLCV(t *testing.T) {
	w := Weekly(sampleDaily())
	if len(w) != 2 {
		t.Fatalf("expected 2 weekly candles, got %d", len(w))
	}

	a := w[0]
	if a.Open != 100 {
		t.Errorf("week A open = %v, want 100 (first day open)", a.Open)
	}
	if a.Close != 118 {
		t.Errorf("week A close = %v, want 118 (last day close)", a.Close)
	}
	if a.High != 120 {
		t.Errorf("week A high = %v, want 120", a.High)
	}
	if a.Low != 95 {
		t.Errorf("week A low = %v, want 95", a.Low)
	}
	if a.Volume != 1000+1200+900+1500+2000 {
		t.Errorf("week A volume = %v, want 6600", a.Volume)
	}

	b := w[1]
	if b.Open != 118 || b.Close != 121 || b.High != 130 || b.Low != 90 {
		t.Errorf("week B OHLC wrong: %+v", b)
	}
}

func TestWeekly_ConfirmedFlag(t *testing.T) {
	w := Weekly(sampleDaily())
	if !w[0].IsConfirmed {
		t.Error("first (older) week should be confirmed")
	}
	if w[len(w)-1].IsConfirmed {
		t.Error("last (most recent) week should be forming, not confirmed")
	}
}

func TestMonthly_GroupsByCalendarMonth(t *testing.T) {
	daily := append(sampleDaily(),
		day(2024, 2, 1, 121, 140, 120, 138, 3000),
		day(2024, 2, 2, 138, 145, 137, 142, 2200),
	)
	m := Monthly(daily)
	if len(m) != 2 {
		t.Fatalf("expected 2 monthly candles, got %d", len(m))
	}
	if m[0].Open != 100 || m[0].Close != 121 {
		t.Errorf("Jan monthly OHLC wrong: %+v", m[0])
	}
	if m[1].Open != 121 || m[1].Close != 142 || m[1].High != 145 {
		t.Errorf("Feb monthly OHLC wrong: %+v", m[1])
	}
	if m[0].IsConfirmed != true || m[1].IsConfirmed != false {
		t.Errorf("confirmed flags wrong: jan=%v feb=%v", m[0].IsConfirmed, m[1].IsConfirmed)
	}
}

func TestAggregate_UnsortedInput(t *testing.T) {
	d := sampleDaily()
	// shuffle order to prove aggregate sorts defensively
	d[0], d[9] = d[9], d[0]
	w := Weekly(d)
	if len(w) != 2 || w[0].Open != 100 {
		t.Fatalf("aggregate did not handle unsorted input: %+v", w)
	}
}

func TestAggregate_Empty(t *testing.T) {
	if got := Weekly(nil); got != nil {
		t.Errorf("expected nil for empty input, got %v", got)
	}
}
