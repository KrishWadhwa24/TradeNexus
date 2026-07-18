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
    }
  ]
}`

func TestParseFeed(t *testing.T) {
	items, err := ParseFeed([]byte(sampleFeed))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("expected 5 items, got %d", len(items))
	}
	by := map[int64]IPO{}
	for _, x := range items {
		by[x.ID] = x
	}

	// Upcoming, no GMP.
	if by[1862].Status != "upcoming" || by[1862].Board != "IPO" || by[1862].Rating != 1 {
		t.Errorf("lohia: %+v", by[1862])
	}
	// Upcoming with GMP 25 / 19.69%.
	x := by[1951]
	if x.Status != "upcoming" || x.GMP != 25 || x.GMPPercent != 19.69 || x.Rating != 3 || x.Size != "170.00 Cr" {
		t.Errorf("xtranet: %+v", x)
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
	// Board parse for SME.
	if by[2129].Board != "BSE SME" {
		t.Errorf("millworks board: %q", by[2129].Board)
	}
	// Date parse.
	if by[1862].CloseDate == nil || by[1862].CloseDate.Format("2006-01-02") != "2026-07-27" {
		t.Errorf("lohia close date wrong: %v", by[1862].CloseDate)
	}
}

func TestTierFor(t *testing.T) {
	cases := map[float64]string{25: "apply", 20: "apply", 19.69: "your_choice", 10: "your_choice", 9.9: "", 0: ""}
	for pct, want := range cases {
		if got := tierFor(pct); got != want {
			t.Errorf("tierFor(%v)=%q want %q", pct, got, want)
		}
	}
	// Upgrade-only ranking.
	if !(tierRank("apply") > tierRank("your_choice")) || !(tierRank("admin_apply") > tierRank("apply")) {
		t.Errorf("tier ranks out of order")
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
