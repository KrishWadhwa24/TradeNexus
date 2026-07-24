package promoter

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		category, mode, txType string
		want                   string
	}{
		{"Promoter", "Market Purchase", "Buy", EventPromoterBuy},
		{"Promoter Group", "Market Sale", "Sell", EventPromoterSell},
		{"Promoter and Director", "Market Purchase", "Buy", EventPromoterBuy},
		{"Director", "Market Purchase", "Buy", EventKMPBuy},
		{"KMP", "Market Sale", "Sell", EventKMPSell},
		{"Designated Person", "Market Purchase", "Buy", ""}, // wrong category
		{"Employee", "ESOP", "Buy", ""},                     // wrong category + mode
		{"Promoter", "Pledge Creation", "Pledge", ""},       // pledge, not a market trade
		{"Promoter", "Gift", "", ""},                        // gift
		{"Promoter", "Off Market", "Buy", ""},               // off-market, not "Market Purchase"
		{"Promoter", "Market Purchase", "Sell", ""},         // mode/type mismatch — don't guess
		{"Trust", "Market Purchase", "Buy", ""},             // untracked category
		{"Connected Person", "Market Sale", "Sell", ""},     // untracked category
	}
	for _, c := range cases {
		if got := classify(c.category, c.mode, c.txType); got != c.want {
			t.Errorf("classify(%q, %q, %q) = %q, want %q", c.category, c.mode, c.txType, got, c.want)
		}
	}
}
