package deals

import (
	"strings"
	"testing"
	"time"
)

// sample CSV mirroring NSE's export (BOM + trailing-space headers + JUL month).
const sampleCSV = "\ufeff\"Date \",\"Symbol \",\"Security Name \",\"Client Name \",\"Buy / Sell \",\"Quantity Traded \",\"Trade Price / Wght. Avg. Price \",\"Remarks \"\n" +
	"\"24-JUL-2026\",\"AASTHA\",\"Aastha Spintex Limited\",\"HRTI PRIVATE LIMITED\",\"BUY\",\"6513085\",\"102.94\",\"-\"\n" +
	"\"24-JUL-2026\",\"AASTHA\",\"Aastha Spintex Limited\",\"HRTI PRIVATE LIMITED\",\"SELL\",\"6555626\",\"102.67\",\"-\"\n" +
	"\"24-JUL-2026\",\"AASTHA\",\"Aastha Spintex Limited\",\"VAXFAB ENTERPRISES LIMITED\",\"BUY\",\"300000\",\"102.40\",\"-\"\n"

func TestParseCSV(t *testing.T) {
	rows, err := parseCSV(Bulk, []byte(sampleCSV))
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	r := rows[0]
	if r.Symbol != "AASTHA" || r.Side != "BUY" || r.Quantity != 6513085 || r.ClientName != "HRTI PRIVATE LIMITED" {
		t.Errorf("row0 mismatch: %+v", r)
	}
	if r.Price < 102.93 || r.Price > 102.95 {
		t.Errorf("price parse wrong: %v", r.Price)
	}
	if got := r.Date.Format("2006-01-02"); got != "2026-07-24" {
		t.Errorf("date parse wrong: %s", got)
	}
	if r.Type != Bulk {
		t.Errorf("type not tagged: %v", r.Type)
	}
}

func TestNetByClient_ChurnVsRealBuyer(t *testing.T) {
	rows, _ := parseCSV(Bulk, []byte(sampleCSV))
	nets := netByClient(rows)
	byName := map[string]ClientNet{}
	for _, c := range nets {
		byName[c.ClientName] = c
	}

	// HRTI round-tripped: net ~ -42,541 shares, tiny net value.
	hrti := byName["HRTI PRIVATE LIMITED"]
	if hrti.NetQty != 6513085-6555626 {
		t.Errorf("HRTI net qty wrong: %d", hrti.NetQty)
	}
	if absValue(hrti) > 5_000_000 { // well under ₹50L, definitely under ₹5cr
		t.Errorf("HRTI should net near zero value, got %v", absValue(hrti))
	}

	// VAXFAB is a genuine net buyer: 300k × 102.40 ≈ ₹3.07cr.
	vax := byName["VAXFAB ENTERPRISES LIMITED"]
	if vax.NetQty != 300000 {
		t.Errorf("VAXFAB net qty wrong: %d", vax.NetQty)
	}
	if vax.NetValue < 3.0e7 || vax.NetValue > 3.1e7 {
		t.Errorf("VAXFAB net value wrong: %v", vax.NetValue)
	}
}

func TestSignificant(t *testing.T) {
	rows, _ := parseCSV(Bulk, []byte(sampleCSV))
	nets := netByClient(rows)

	// At ₹5cr, this stock has NO qualifying client (VAXFAB is only ₹3.07cr) → filtered.
	if significant(nets, 50_000_000) {
		t.Error("expected not significant at ₹5cr threshold")
	}
	// At ₹1cr, VAXFAB qualifies.
	if !significant(nets, 10_000_000) {
		t.Error("expected significant at ₹1cr threshold")
	}
	// Threshold 0 disables the filter.
	if !significant(nets, 0) {
		t.Error("threshold 0 should always be significant")
	}
}

func TestSplitBuyersSellers(t *testing.T) {
	nets := []ClientNet{
		{ClientName: "A", NetQty: 100, NetValue: 5000},
		{ClientName: "B", NetQty: -50, NetValue: -3000},
		{ClientName: "C", NetQty: 0, NetValue: 0}, // flat — dropped
		{ClientName: "D", NetQty: -80, NetValue: -9000},
	}
	// pre-sort desc by net value like netByClient would.
	buyers, sellers := splitBuyersSellers([]ClientNet{nets[0], nets[2], nets[1], nets[3]})
	if len(buyers) != 1 || buyers[0].ClientName != "A" {
		t.Errorf("buyers wrong: %+v", buyers)
	}
	// sellers ordered by largest magnitude first: D (-9000) before B (-3000).
	if len(sellers) != 2 || sellers[0].ClientName != "D" || sellers[1].ClientName != "B" {
		t.Errorf("sellers order wrong: %+v", sellers)
	}
}

func TestFormatDealMessage(t *testing.T) {
	rows, _ := parseCSV(Bulk, []byte(sampleCSV))
	nets := netByClient(rows)
	day := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	msg := formatDealMessage(Bulk, rows, nets, day, bulkTopN)

	for _, want := range []string{
		"BULK DEAL — AASTHA",
		"Top Buyers (net)",
		"VAXFAB ENTERPRISES LIMITED",
		"Top Sellers (net)",
		"Bought:",
		"Sold:",
		"24-Jul-2026",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q\n---\n%s", want, msg)
		}
	}
}

func TestGroupInt(t *testing.T) {
	cases := map[int64]string{
		0:       "0",
		999:     "999",
		1000:    "1,000",
		100000:  "1,00,000",
		6513085: "65,13,085",
	}
	for in, want := range cases {
		if got := groupInt(in); got != want {
			t.Errorf("groupInt(%d) = %s, want %s", in, got, want)
		}
	}
}

func TestRupees(t *testing.T) {
	cases := map[float64]string{
		30720000: "₹3.07 Cr",
		4560000:  "₹45.6 L",
		1234:     "₹1,234",
	}
	for in, want := range cases {
		if got := rupees(in); got != want {
			t.Errorf("rupees(%v) = %s, want %s", in, got, want)
		}
	}
}
