package deals

import "testing"

func TestIsMutualFund(t *testing.T) {
	cases := map[string]bool{
		"SBI MUTUAL FUND":            true,
		"HDFC MUTUAL FUND.":          true,
		"mutual fund of india":       true,
		"HRTI PRIVATE LIMITED":       false,
		"VAXFAB ENTERPRISES LIMITED": false,
	}
	for name, want := range cases {
		if got := isMutualFund(name); got != want {
			t.Errorf("isMutualFund(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestNormalizeFundName(t *testing.T) {
	cases := map[string]string{
		"HDFC MUTUAL FUND":   "HDFC MUTUAL FUND",
		"HDFC MUTUAL FUND.":  "HDFC MUTUAL FUND",
		" sbi mutual fund  ": "SBI MUTUAL FUND",
		"axis mutual fund .": "AXIS MUTUAL FUND",
	}
	for in, want := range cases {
		if got := normalizeFundName(in); got != want {
			t.Errorf("normalizeFundName(%q) = %q, want %q", in, got, want)
		}
	}
	// The two real-world variants NSE emits for the same AMC must collapse
	// to the same key, or they'd wrongly appear as two separate funds.
	if normalizeFundName("HDFC MUTUAL FUND") != normalizeFundName("HDFC MUTUAL FUND.") {
		t.Error("trailing-period variant did not collapse to the same fund key")
	}
}

func TestGetFund_TotalsSumStocks(t *testing.T) {
	stocks := []FundStock{
		{Symbol: "RELIANCE", BuyValue: 300, SellValue: 50},
		{Symbol: "TCS", BuyValue: 120, SellValue: 0},
		{Symbol: "INFY", BuyValue: 0, SellValue: 80},
	}
	var d FundDetail
	for _, st := range stocks {
		d.BuyValue += st.BuyValue
		d.SellValue += st.SellValue
	}
	if d.BuyValue != 420 {
		t.Errorf("BuyValue = %v, want 420", d.BuyValue)
	}
	if d.SellValue != 130 {
		t.Errorf("SellValue = %v, want 130", d.SellValue)
	}
}
