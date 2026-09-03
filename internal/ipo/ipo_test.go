package ipo

import (
	"testing"
	"time"
)

const sampleFeed = `{
  "reportTableData": [
    {
      "~orderby1": 9999,
      "Name": "<a href=\"/gmp/lohia-corp-ipo/1862/\" title=\"Lohia Corp\">Lohia Corp</a> <span class=\"badge rounded-pill bg-secondary d-inline ms-2\">IPO</span><span class=\"badge rounded-pill bg-warning d-inline ms-2\">U</span>",
      "GMP": "&#8377;<b>--</b> (%)",
      "Rating": "<span>&#128293;</span>",
      "Sub": "-",
      "Price (₹)": "",
      "IPO Size": "-",
      "Lot": "",
      "~P/E": "--",
      "~id": 1862,
      "~gmp_percent_calc": "",
      "~Srt_Open": "2026-07-23",
      "~Srt_Close": "2026-07-27",
      "~Str_Listing": "2026-07-30",
      "~urlrewrite_folder_name": "/gmp/lohia-corp-ipo/1862/",
      "~IPO_Category": "IPO",
      "~ipo_name": "Lohia Corp"
    },
    {
      "Name": "<a>Xtranet Technologies</a> <span class=\"badge rounded-pill bg-secondary d-inline ms-2\">IPO</span><span class=\"badge rounded-pill bg-warning d-inline ms-2\">U</span>",
      "GMP": "&#8377;<b>25</b> (19.69%)",
      "Rating": "<span>&#128293;&#128293;&#128293;</span>",
      "Sub": "-",
      "Price (₹)": "127",
      "IPO Size": "&#8377;170.00 Cr",
      "Lot": "110",
      "~id": 1951,
      "~gmp_percent_calc": "19.69",
      "~Srt_Close": "2026-07-27",
      "~IPO_Category": "IPO",
      "~ipo_name": "Xtranet Technologies"
    },
    {
      "Name": "<a>Caliber Mining</a> <span class=\"badge rounded-pill bg-secondary d-inline ms-2\">IPO</span><span class=\"badge rounded-pill bg-success d-inline ms-2\">O</span>",
      "GMP": "&#8377;<b>118</b> (27.83%)",
      "Rating": "<span>&#128293;&#128293;&#128293;&#128293;</span>",
      "Sub": "1.31x",
      "Price (₹)": "424",
      "~id": 1610,
      "~gmp_percent_calc": "27.83",
      "~Srt_Close": "2026-07-21",
      "~IPO_Category": "IPO",
      "~ipo_name": "Caliber Mining"
    },
    {
      "Name": "<a>Millworks</a> <span class=\"badge rounded-pill bg-secondary d-inline ms-2\">BSE SME</span><span class=\"badge rounded-pill bg-primary d-inline ms-2\">C</span>",
      "GMP": "&#8377;<b>401</b> (121.15%)",
      "~id": 2129,
      "~gmp_percent_calc": "121.15",
      "~IPO_Category": "SME",
      "~ipo_name": "Millworks Technologies"
    },
    {
      "Name": "<a>Devson</a> <span class=\"badge rounded-pill bg-secondary d-inline ms-2\">BSE SME</span><span class='text-success d-inline ms-2'><small><b>L@196.15 (66.23%)</b></small></span>",
      "GMP": "&#8377;<b>49</b> (41.53%)",
      "~id": 2204,
      "~gmp_percent_calc": "41.53",
      "~IPO_Category": "SME",
      "~ipo_name": "Devson Catalyst"
    },
    {
      "Name": "<a>Indo-MIM</a> <span class=\"badge rounded-pill bg-secondary d-inline ms-2\">IPO</span><span class=\"badge rounded-pill bg-danger d-inline ms-2\">CT</span>",
      "GMP": "&#8377;<b>193</b> (39.79%)",
      "~id": 1594,
      "~gmp_percent_calc": "39.79",
      "~Srt_Close": "2026-07-27",
      "~IPO_Category": "IPO",
      "~ipo_name": "Indo-MIM"
    }
  ]
}`

func TestParseFeed(t *testing.T) {
	items, err := ParseFeed([]byte(sampleFeed))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(items) != 6 {
		t.Fatalf("expected 6 items, got %d", len(items))
	}
	by := map[int64]IPO{}
	for _, x := range items {
		by[x.ID] = x
	}

	// Upcoming, no GMP.
	if by[1862].Status != "upcoming" || by[1862].Board != "IPO" || by[1862].Rating != 1 {
		t.Errorf("lohia: %+v", by[1862])
	}
	// Upcoming with GMP 25 / 19.69%. Price MUST parse despite the "Price (₹)" key.
	x := by[1951]
	if x.Status != "upcoming" || x.GMP != 25 || x.GMPPercent != 19.69 || x.Rating != 3 || x.Size != "170.00 Cr" {
		t.Errorf("xtranet: %+v", x)
	}
	if x.Price != "127" {
		t.Errorf("xtranet price should parse to 127, got %q", x.Price)
	}
	// Open.
	if by[1610].Status != "open" || by[1610].GMP != 118 {
		t.Errorf("caliber: %+v", by[1610])
	}
	// Closed and Listed must be detected (so the service drops them).
	if by[2129].Status != "closed" {
		t.Errorf("millworks should be closed, got %q", by[2129].Status)
	}
	if by[2204].Status != "listed" {
		t.Errorf("devson should be listed, got %q", by[2204].Status)
	}
	// "CT" (closing today) badge must count as still open, not dropped.
	if by[1594].Status != "open" {
		t.Errorf("indo-mim (CT badge) should be open, got %q", by[1594].Status)
	}
	// Board parse for SME.
	if by[2129].Board != "BSE SME" {
		t.Errorf("millworks board: %q", by[2129].Board)
	}
	// Date parse.
	if by[1862].CloseDate == nil || by[1862].CloseDate.Format("2006-01-02") != "2026-07-27" {
		t.Errorf("lohia close date wrong: %v", by[1862].CloseDate)
	}
}

// sampleSubscriptionFeed is trimmed from the real InvestorGain subscription
// report (report id 333) — Indo-MIM (~id 1594, QIB well under threshold,
// anchor ✅) and Caliber Mining (~id 1610, QIB well over threshold, same id
// used by sampleFeed's "open" IPO above so the two feeds merge realistically).
const sampleSubscriptionFeed = `{
  "reportTableData": [
    {
      "Name": "<a>Indo-MIM</a>",
      "Total": "<b>1.14</b><br><small><b>23rd Jul 18:55</b></small>",
      "QIB": "0.18",
      "SHNI": "3.61",
      "BHNI": "2.75",
      "NII": "3.04",
      "RII": "0.88",
      "Anchor": "<span style=\"color:green;font-weight:bold;\">✅</span>",
      "~id": 1594
    },
    {
      "Name": "<a>Caliber Mining</a>",
      "Total": "<b>154.66</b><br><small><b>21st Jul 18:55</b></small>",
      "QIB": "253.88",
      "SHNI": "227.37",
      "BHNI": "309.31",
      "NII": "281.99",
      "RII": "43.4",
      "Anchor": "<span style=\"color:green;font-weight:bold;\">✅</span>",
      "~id": 1610
    },
    {
      "Name": "<a>No Anchor</a>",
      "Total": "<b>10.85</b><br>",
      "QIB": "",
      "SHNI": "",
      "BHNI": "",
      "NII": "2.86",
      "RII": "18.83",
      "Anchor": "<span style=\"color:red;font-weight:bold;\">❌</span>",
      "~id": 2214
    }
  ]
}`

func TestParseSubscriptionFeed(t *testing.T) {
	subs, err := ParseSubscriptionFeed([]byte(sampleSubscriptionFeed))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(subs) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(subs))
	}

	indoMim := subs[1594]
	if indoMim.QIB != 0.18 || indoMim.Total != 1.14 || !indoMim.AnchorPositive {
		t.Errorf("indo-mim: %+v", indoMim)
	}
	caliber := subs[1610]
	if caliber.QIB != 253.88 || caliber.Total != 154.66 || !caliber.AnchorPositive {
		t.Errorf("caliber: %+v", caliber)
	}
	noAnchor := subs[2214]
	if noAnchor.QIB != 0 || noAnchor.NII != 2.86 || noAnchor.AnchorPositive {
		t.Errorf("no-anchor: %+v", noAnchor)
	}
}

func TestQIBGateThreshold(t *testing.T) {
	// Mirrors the AND-gate in RunClosingDaySignals: both the GMP tier and
	// QIB > qibAlertThreshold must hold before a signal is allowed.
	cases := []struct {
		gmpPct, qib float64
		wantSignal  bool
	}{
		{25, 253.88, true}, // Caliber-like: strong GMP tier + strong QIB
		{25, 0.18, false},  // Indo-MIM-like: strong GMP tier but weak QIB
		{5, 253.88, false}, // strong QIB but GMP tier doesn't qualify
		{25, 5.0, false},   // QIB exactly at threshold — must exceed, not equal
		{25, 5.01, true},
	}
	for _, c := range cases {
		tier := tierFor(c.gmpPct)
		gotSignal := tier != "" && c.qib > qibAlertThreshold
		if gotSignal != c.wantSignal {
			t.Errorf("gmp=%.2f qib=%.2f: signal=%v, want %v", c.gmpPct, c.qib, gotSignal, c.wantSignal)
		}
	}
}

func TestTierFor(t *testing.T) {
	cases := map[float64]string{25: "apply", 20: "apply", 19.69: "your_choice", 10: "your_choice", 9.9: "", 0: ""}
	for pct, want := range cases {
		if got := tierFor(pct); got != want {
			t.Errorf("tierFor(%v)=%q want %q", pct, got, want)
		}
	}
}

func TestIsSME(t *testing.T) {
	if !isSME(IPO{Board: "BSE SME"}) || !isSME(IPO{Board: "NSE SME"}) || !isSME(IPO{Category: "SME"}) {
		t.Error("SME issues should be detected")
	}
	if isSME(IPO{Board: "IPO", Category: "IPO"}) {
		t.Error("mainboard IPO must not be flagged SME")
	}
}

func TestFinancialYear(t *testing.T) {
	if got := financialYear(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)); got != "2026-27" {
		t.Errorf("Jul 2026 FY = %q, want 2026-27", got)
	}
	if got := financialYear(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); got != "2025-26" {
		t.Errorf("Jan 2026 FY = %q, want 2025-26", got)
	}
}

func TestFormatIPOMessage(t *testing.T) {
	cd := time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC)
	x := IPO{Name: "Xtranet Technologies", Board: "IPO", GMP: 25, GMPPercent: 19.69,
		Subscription: "1.2x", Price: "127", Lot: "110", CloseDate: &cd}
	msg := formatIPOMessage(x, "Apply for IPO", true)
	// GMP 25 × lot 110 = 2750 est. profit per lot.
	for _, want := range []string{"Xtranet Technologies", "GMP: ₹25 (19.69%)", "Est. profit: ₹2750 / lot (GMP × 110)", "Closes: 27 Jul 2026 (today)", "Signal: Apply for IPO"} {
		if !contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
