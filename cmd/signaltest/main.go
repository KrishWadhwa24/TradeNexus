package main

import (
	"encoding/json"
	"fmt"
	"time"

	"tradenexus/internal/market"
	"tradenexus/internal/scanner"
)

type result struct {
	Scanner string         `json:"scanner"`
	Fired   bool           `json:"fired"`
	Details map[string]any `json:"details"`
}

func main() {
	results := []result{
		runPine(),
		runWeekly(),
		runDowntrendBreakout(),
		runRectangle(),
		runCupHandle(),
	}

	out, _ := json.MarshalIndent(results, "", "  ")
	fmt.Println(string(out))

	ok := true
	for _, r := range results {
		if !r.Fired {
			ok = false
		}
	}
	if !ok {
		panic("one or more fake scanner charts did not fire")
	}
}

func runPine() result {
	candles := fakePineCandles()
	sig := scanner.ScanPine(candles, scanner.DefaultPineConfig())
	return result{
		Scanner: "pine",
		Fired:   sig.Buy,
		Details: map[string]any{"buy": sig.Buy, "sell": sig.Sell, "reasons": sig.Reasons},
	}
}

func runWeekly() result {
	candles := fakeWeeklyCandles()
	sig := scanner.ScanWeekly(candles)
	return result{
		Scanner: "weekly",
		Fired:   sig.Confidence > 0,
		Details: map[string]any{"confidence": sig.Confidence, "fired": sig.Fired, "details": sig.Details},
	}
}

func runDowntrendBreakout() result {
	sig := scanner.ScanDowntrendBreakout(fakeDowntrendBreakoutCandles())
	return result{
		Scanner: "pattern_downtrend_breakout",
		Fired:   sig.Buy,
		Details: map[string]any{"buy": sig.Buy, "reasons": sig.Reasons},
	}
}

func runRectangle() result {
	sig := scanner.ScanRectangleConsolidation(fakeRectangleCandles())
	return result{
		Scanner: "pattern_rectangle",
		Fired:   sig.Buy,
		Details: map[string]any{"buy": sig.Buy, "reasons": sig.Reasons},
	}
}

func runCupHandle() result {
	sig := scanner.ScanCupAndHandle(fakeCupHandleCandles())
	return result{
		Scanner: "pattern_cup_handle",
		Fired:   sig.Buy,
		Details: map[string]any{"buy": sig.Buy, "reasons": sig.Reasons},
	}
}

func fakePineCandles() []market.Candle {
	var out []market.Candle
	for i := 0; i < 60; i++ {
		base := 100.0 + float64(i)*0.22
		out = append(out, candle(i, base-0.2, base+0.8, base-0.8, base, 1000))
	}
	last := len(out) - 1
	out[last] = candle(last, 111, 123, 110, 122, 6000)
	return out
}

func fakeWeeklyCandles() []market.Candle {
	var out []market.Candle
	for i := 0; i < 60; i++ {
		wave := float64((i % 6) - 3)
		base := 100.0 + wave
		out = append(out, candle(i, base-0.5, base+1.5, base-1.5, base, 1000))
	}
	out[56] = candle(56, 101, 103, 99, 101, 1000)
	out[57] = candle(57, 101, 104, 100, 102, 1000)
	out[58] = candle(58, 102, 105, 101, 103, 1000)
	out[59] = candle(59, 103, 112, 102, 111, 3000)
	return out
}

func fakeDowntrendBreakoutCandles() []market.Candle {
	out := flatCandles(50, 55, 1000)
	setPivotHigh(out, 5, 100)
	setPivotHigh(out, 15, 90)
	setPivotHigh(out, 25, 80)
	setPivotHigh(out, 35, 70)
	for i := 29; i < 49; i++ {
		out[i].Close = 54 + float64(i-29)*0.12
	}
	out[49] = candle(49, 58, 62, 57, 60, 5000)
	return out
}

func fakeRectangleCandles() []market.Candle {
	out := flatCandles(56, 95, 900)
	setPivotHigh(out, 20, 100)
	setPivotLow(out, 25, 90)
	setPivotHigh(out, 30, 101)
	setPivotLow(out, 35, 91)
	setPivotHigh(out, 40, 100)
	setPivotLow(out, 45, 90.5)
	for i := 20; i < 25; i++ {
		out[i].Volume = 2200
	}
	for i := 50; i < 55; i++ {
		out[i].Volume = 500
	}
	out[55] = candle(55, 101, 104, 100, 103, 5000)
	return out
}

func fakeCupHandleCandles() []market.Candle {
	out := flatCandles(36, 80, 1000)
	setPivotHigh(out, 5, 100)
	setPivotLow(out, 13, 60)
	setPivotHigh(out, 21, 98)
	setPivotLow(out, 27, 90)
	for i := 5; i <= 21; i++ {
		out[i].Volume = 1600
	}
	for i := 21; i <= 27; i++ {
		out[i].Volume = 700
	}
	out[35] = candle(35, 100, 104, 99, 102, 5000)
	return out
}

func flatCandles(n int, price float64, volume int64) []market.Candle {
	out := make([]market.Candle, n)
	for i := range out {
		out[i] = candle(i, price, price+1, price-1, price, volume)
	}
	return out
}

func setPivotHigh(c []market.Candle, i int, price float64) {
	c[i] = candle(i, price-2, price, price-4, price-1, c[i].Volume)
	for _, j := range []int{i - 2, i - 1, i + 1, i + 2} {
		if j >= 0 && j < len(c) {
			c[j].High = price - 5
			if c[j].Close > c[j].High {
				c[j].Close = c[j].High - 1
			}
		}
	}
}

func setPivotLow(c []market.Candle, i int, price float64) {
	c[i] = candle(i, price+2, price+4, price, price+1, c[i].Volume)
	for _, j := range []int{i - 2, i - 1, i + 1, i + 2} {
		if j >= 0 && j < len(c) {
			c[j].Low = price + 5
			if c[j].Close < c[j].Low {
				c[j].Close = c[j].Low + 1
			}
		}
	}
}

func candle(i int, open, high, low, close float64, volume int64) market.Candle {
	return market.Candle{
		Time:   time.Date(2026, 1, 1+i, 0, 0, 0, 0, market.IST),
		Open:   open,
		High:   high,
		Low:    low,
		Close:  close,
		Volume: volume,
	}
}
