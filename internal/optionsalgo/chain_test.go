package optionsalgo

import "testing"

func TestNearestATMStrikes(t *testing.T) {
	strikes := []float64{23800, 23850, 23900, 23950, 24000, 24050, 24100, 24150, 24200, 24250, 24300, 24350, 24400}
	got := NearestATMStrikes(strikes, 23905, 5)
	// ATM = 23900 (closest to 23905), 5 either side -> 23650(missing, clamps)..24150
	want := []float64{23800, 23850, 23900, 23950, 24000, 24050, 24100, 24150}
	if len(got) != len(want) {
		t.Fatalf("got %d strikes, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestNearestATMStrikes_ClampsAtEdges(t *testing.T) {
	strikes := []float64{100, 200, 300}
	got := NearestATMStrikes(strikes, 100, 5)
	if len(got) != 3 {
		t.Errorf("got %d strikes, want 3 (clamped to available range)", len(got))
	}
}

func TestNearestATMStrikes_Empty(t *testing.T) {
	if got := NearestATMStrikes(nil, 100, 5); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestSpreadPercent(t *testing.T) {
	q := OptionQuote{Bid: 119.65, Ask: 120.35}
	got := q.SpreadPercent()
	want := (120.35 - 119.65) / ((120.35 + 119.65) / 2) * 100
	if !closeEnough(got, want) {
		t.Errorf("SpreadPercent = %v, want %v", got, want)
	}
}

func TestSpreadPercent_NoDepth(t *testing.T) {
	q := OptionQuote{Bid: 0, Ask: 0}
	if got := q.SpreadPercent(); got != 0 {
		t.Errorf("SpreadPercent = %v, want 0 for empty depth", got)
	}
}

func TestSelectByDelta_PicksClosestToTarget(t *testing.T) {
	candidates := []OptionQuote{
		{TradingSymbol: "far-otm", Delta: 0.20},
		{TradingSymbol: "in-band-low", Delta: 0.56},
		{TradingSymbol: "in-band-best", Delta: 0.61}, // closest to 0.60
		{TradingSymbol: "in-band-high", Delta: 0.69},
		{TradingSymbol: "far-itm", Delta: 0.95},
	}
	got, ok := SelectByDelta(candidates, 0.60, 0.55, 0.70)
	if !ok {
		t.Fatal("expected a selection")
	}
	if got.TradingSymbol != "in-band-best" {
		t.Errorf("selected %s, want in-band-best", got.TradingSymbol)
	}
}

func TestSelectByDelta_PE_NegativeDelta(t *testing.T) {
	candidates := []OptionQuote{
		{TradingSymbol: "pe-a", Delta: -0.61},
		{TradingSymbol: "pe-b", Delta: -0.20},
	}
	got, ok := SelectByDelta(candidates, 0.60, 0.55, 0.70)
	if !ok || got.TradingSymbol != "pe-a" {
		t.Errorf("got %+v, ok=%v, want pe-a selected via abs(delta)", got, ok)
	}
}

func TestSelectByDelta_NoneQualify(t *testing.T) {
	candidates := []OptionQuote{{Delta: 0.10}, {Delta: 0.95}}
	_, ok := SelectByDelta(candidates, 0.60, 0.55, 0.70)
	if ok {
		t.Error("expected no selection when nothing is in the delta band")
	}
}

func TestLiquidityCheck_WideSpreadRejected(t *testing.T) {
	q := OptionQuote{Bid: 100, Ask: 110, Volume: 1000} // spread ~9.5%
	ok, reason := LiquidityCheck(q, 500, 1.0, 1.2)
	if ok {
		t.Error("expected rejection for wide spread")
	}
	if reason != "spread too wide" {
		t.Errorf("reason = %q", reason)
	}
}

func TestLiquidityCheck_LowVolumeRejected(t *testing.T) {
	q := OptionQuote{Bid: 100, Ask: 100.5, Volume: 100} // tight spread, low volume
	ok, reason := LiquidityCheck(q, 1000, 1.0, 1.2)     // needs >= 1200
	if ok {
		t.Error("expected rejection for low volume")
	}
	if reason != "volume below liquidity threshold" {
		t.Errorf("reason = %q", reason)
	}
}

func TestLiquidityCheck_Passes(t *testing.T) {
	q := OptionQuote{Bid: 100, Ask: 100.5, Volume: 2000}
	ok, reason := LiquidityCheck(q, 1000, 1.0, 1.2)
	if !ok {
		t.Errorf("expected pass, got reason %q", reason)
	}
}

func TestAverageVolume(t *testing.T) {
	quotes := []OptionQuote{{Volume: 100}, {Volume: 200}, {Volume: 300}}
	if got := AverageVolume(quotes); got != 200 {
		t.Errorf("AverageVolume = %v, want 200", got)
	}
}

func TestAverageVolume_Empty(t *testing.T) {
	if got := AverageVolume(nil); got != 0 {
		t.Errorf("AverageVolume = %v, want 0", got)
	}
}

// TestSelectByDelta_DeclinesWhenGreeksMissing pins the safety property that
// makes non-fatal Greeks safe (see BuildOptionChain): when Angel's Greeks
// endpoint is down, every quote carries delta 0, which is outside any sane
// band — selection must decline rather than fall back to picking something
// arbitrary and trading blind.
func TestSelectByDelta_DeclinesWhenGreeksMissing(t *testing.T) {
	noGreeks := []OptionQuote{
		{TradingSymbol: "A", OptionType: "CE", LTP: 100}, // Delta left at 0
		{TradingSymbol: "B", OptionType: "CE", LTP: 150},
		{TradingSymbol: "C", OptionType: "CE", LTP: 200},
	}
	if got, ok := SelectByDelta(noGreeks, 0.60, 0.55, 0.70); ok {
		t.Fatalf("expected no selection when every delta is 0 (greeks unavailable), got %q", got.TradingSymbol)
	}
}

// TestLiquidityCheck_RejectsEmptyOrderBook is the regression test for a real
// bug: SpreadPercent returns 0 when there's no two-sided quote, and 0
// trivially satisfies "spread <= max", so a contract nobody was quoting
// silently passed the spread gate as if it had a perfect zero spread.
func TestLiquidityCheck_RejectsEmptyOrderBook(t *testing.T) {
	cases := []struct {
		name string
		q    OptionQuote
	}{
		{"no quotes at all", OptionQuote{LTP: 120, Volume: 100000}},
		{"bid only", OptionQuote{LTP: 120, Bid: 119, Volume: 100000}},
		{"ask only", OptionQuote{LTP: 120, Ask: 121, Volume: 100000}},
	}
	for _, c := range cases {
		// avgVolume 0 on purpose: the volume leg no-ops there, so the
		// spread leg is the only thing standing between this contract and
		// a real order.
		if ok, _ := LiquidityCheck(c.q, 0, 1.0, 1.2); ok {
			t.Errorf("%s: passed the liquidity gate — an empty order book is the most illiquid case there is", c.name)
		}
	}
	// A genuinely liquid contract must still pass.
	good := OptionQuote{LTP: 120, Bid: 119.9, Ask: 120.1, Volume: 100000}
	if ok, why := LiquidityCheck(good, 50000, 1.0, 1.2); !ok {
		t.Errorf("a tight-spread, high-volume contract must pass, got %q", why)
	}
}
