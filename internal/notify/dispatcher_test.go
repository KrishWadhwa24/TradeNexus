package notify

import (
	"strings"
	"testing"
	"time"

	"tradenexus/internal/signals"
)

func fptr(v float64) *float64 { return &v }
func iptr(v int) *int         { return &v }

func TestFormatMessage_Pine(t *testing.T) {
	sig := signals.Signal{
		Source:      "pine",
		ScannerName: "pine",
		Timeframe:   "1D",
		Direction:   "BUY",
		CandleDate:  time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		RSI:         fptr(76.2),
		Metrics: map[string]float64{
			"breakout_len": 20,
			"body_atr":     0.82,
			"rel_volume":   1.89,
		},
	}
	msg := formatMessage(sig, "ADANIENSOL-EQ", 1540.10)

	want := []string{
		"🟢 BUY SIGNAL — ADANIENSOL-EQ (1D)",
		"📊 Strategy: Pine Script Momentum",
		"⏱ Timeframe: 1D — Swing Confirmation",
		"📈 Breakout: Close crossed above 20-bar high",
		"💪 Candle: Body strength 0.82x ATR",
		"📊 Volume: Relative volume 1.89x",
		"🔥 RSI: 76.2 (Strong bullish)",
		"📐 Trend: EMA 10 > 20 > SMA 40 (Bullish stack)",
		"⚡ Conviction: HIGH",
		"💰 Price: ₹1540.10",
		"🕐 Candle Close: 27 May 2026, 00:00 IST",
	}
	for _, w := range want {
		if !strings.Contains(msg, w) {
			t.Errorf("message missing %q\n---\n%s", w, msg)
		}
	}
}

func TestFormatMessage_WeeklyAndPatterns(t *testing.T) {
	weekly := signals.Signal{
		Source: "weekly", ScannerName: "weekly_1,weekly_3", Timeframe: "1W",
		Direction: "BUY", CandleDate: time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC),
		Confidence: iptr(2), RSI: fptr(58),
	}
	m := formatMessage(weekly, "TCS-EQ", 3900)
	if !strings.Contains(m, "🎯 Scanners: 2 of 4") || !strings.Contains(m, "⚡ Conviction: MEDIUM") {
		t.Errorf("weekly format wrong:\n%s", m)
	}
	if !strings.Contains(m, "52-wk high breakout + EMA stack") {
		t.Errorf("weekly matched labels missing:\n%s", m)
	}

	pat := signals.Signal{
		Source: "patterns", ScannerName: "pattern_cup_handle", Timeframe: "1D",
		Direction: "BUY", CandleDate: time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
		Confidence: iptr(82),
	}
	mp := formatMessage(pat, "INFY-EQ", 1600)
	if !strings.Contains(mp, "🔎 Pattern: Cup & Handle") || !strings.Contains(mp, "⚡ Conviction: HIGH") {
		t.Errorf("pattern format wrong:\n%s", mp)
	}
}

func TestPineConviction(t *testing.T) {
	high := pineConviction(map[string]float64{"rel_volume": 2.6, "body_atr": 1.1}, fptr(78), true)
	if high != "HIGH" {
		t.Errorf("expected HIGH, got %s", high)
	}
	low := pineConviction(map[string]float64{"rel_volume": 1.2}, fptr(52), true)
	if low != "LOW" {
		t.Errorf("expected LOW, got %s", low)
	}
}

func TestRSILabel(t *testing.T) {
	if got := rsiLabel(76, true); got != "Strong bullish" {
		t.Errorf("buy 76 → %s", got)
	}
	if got := rsiLabel(28, false); got != "Strong bearish" {
		t.Errorf("sell 28 → %s", got)
	}
}
