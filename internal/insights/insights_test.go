package insights

import "testing"

func TestRet_DirectionalAndMaturity(t *testing.T) {
	// index 0 = entry (100); +5d = 110, +10d = 90.
	closes := make([]float64, 11)
	for i := range closes {
		closes[i] = 100
	}
	closes[5] = 110
	closes[10] = 90

	// BUY: +10% at 5d, -10% at 10d.
	if r := ret(closes, 5, 100, true); r == nil || *r < 9.99 || *r > 10.01 {
		t.Errorf("buy 5d: got %v want +10", r)
	}
	if r := ret(closes, 10, 100, true); r == nil || *r < -10.01 || *r > -9.99 {
		t.Errorf("buy 10d: got %v want -10", r)
	}
	// SELL inverts: a price rise is a loss.
	if r := ret(closes, 5, 100, false); r == nil || *r < -10.01 || *r > -9.99 {
		t.Errorf("sell 5d: got %v want -10", r)
	}
	// 20d hasn't matured (only 11 closes) → nil.
	if r := ret(closes, 20, 100, true); r != nil {
		t.Errorf("20d should be nil (unmatured), got %v", *r)
	}
}

func TestBareSymbol(t *testing.T) {
	cases := map[string]string{
		"RELIANCE-EQ":   "RELIANCE",
		"AASTHA-BE":     "AASTHA",
		"BAJAJ-AUTO-EQ": "BAJAJ-AUTO", // hyphenated NSE name preserved
		"SUDEEPPHRM":    "SUDEEPPHRM", // no suffix
		"infy-eq":       "INFY",       // case-insensitive
	}
	for in, want := range cases {
		if got := bareSymbol(in); got != want {
			t.Errorf("bareSymbol(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSourcesAndScore(t *testing.T) {
	c := &ConfluenceStock{ScannerBuy: true, PromoterBuy: true, BulkBuy: true}
	got := sourcesOf(c)
	if len(got) != 3 {
		t.Fatalf("want 3 sources, got %v", got)
	}
	if got[0] != "Scanner BUY" || got[2] != "Bulk net-buy" {
		t.Errorf("unexpected source order: %v", got)
	}
}

func TestSortByScore(t *testing.T) {
	list := []ConfluenceStock{
		{Symbol: "B", Score: 2},
		{Symbol: "A", Score: 4},
		{Symbol: "C", Score: 2},
	}
	sortByScore(list)
	if list[0].Symbol != "A" || list[1].Symbol != "B" || list[2].Symbol != "C" {
		t.Errorf("bad order: %+v", list)
	}
}
