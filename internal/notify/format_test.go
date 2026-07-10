package notify

import (
	"strings"
	"testing"
	"time"

	"tradenexus/internal/signals"
)

func TestTfLabel(t *testing.T) {
	cases := map[string]string{"1D": "Daily", "1W": "Weekly", "1M": "Monthly", "??": "??"}
	for in, want := range cases {
		if got := tfLabel(in); got != want {
			t.Errorf("tfLabel(%q)=%q want %q", in, got, want)
		}
	}
}

func TestHumanVol(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{950, "950"},
		{12_500, "12.5K"},
		{845_000, "8.45L"},
		{23_400_000, "2.34Cr"},
	}
	for _, c := range cases {
		if got := humanVol(c.in); got != c.want {
			t.Errorf("humanVol(%v)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestFormatMessage_WeeklyContainsKeyFields(t *testing.T) {
	conf := 3
	rsi := 63.4
	vol := 1_250_000.0
	sig := signals.Signal{
		Symbol:      "RELIANCE-EQ",
		Source:      "weekly",
		ScannerName: "weekly_1,weekly_3",
		Timeframe:   "1W",
		Direction:   "BUY",
		CandleDate:  time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC),
		Confidence:  &conf,
		RSI:         &rsi,
		Volume:      &vol,
	}
	msg := formatMessage(sig, "RELIANCE-EQ", 2845.30)

	for _, want := range []string{"RELIANCE-EQ", "Weekly", "BUY", "CMP", "2845.30", "3/4", "63.4", "weekly_1,weekly_3"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q\n---\n%s", want, msg)
		}
	}
	if !strings.HasPrefix(msg, "🟢") {
		t.Errorf("BUY message should start with green marker, got: %q", msg)
	}
}

func TestFormatMessage_PineNoConfidenceNoCMPWhenZero(t *testing.T) {
	sig := signals.Signal{
		Symbol:    "TCS-EQ",
		Source:    "pine",
		Timeframe: "1D",
		Direction: "SELL",
		CandleDate: time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
	}
	msg := formatMessage(sig, "TCS-EQ", 0) // cmp=0 → omitted
	if strings.Contains(msg, "CMP") {
		t.Errorf("CMP line should be omitted when price is 0:\n%s", msg)
	}
	if !strings.Contains(msg, "Chase Momentum") {
		t.Errorf("pine signal should show friendly scanner name:\n%s", msg)
	}
	if !strings.HasPrefix(msg, "🔴") {
		t.Errorf("SELL message should start with red marker")
	}
}
