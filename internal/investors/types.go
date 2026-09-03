// Package investors tracks a curated list of well-known Indian stock-market
// investors against NSE's quarterly shareholding-pattern (SHP) XBRL feed and
// surfaces which tracked companies they currently hold a disclosed stake in.
package investors

import (
	"regexp"
	"strings"
	"time"
)

// Investor is one curated big-investor this feature tracks. Name always
// matches a shareholder's raw NSE-disclosed name exactly (normalized);
// Aliases are known entity/HUF/fund names the same person invests through,
// matched as a substring since they're sourced from public reporting rather
// than a verified filing.
//
// ponytail: hand-curated from public reporting (Trendlyne/press coverage),
// not an exhaustive or authoritative registry — extend as new aliases are
// confirmed against real filings.
type Investor struct {
	Name    string
	Aliases []string
}

var Tracked = []Investor{
	{Name: "Radhakishan Damani", Aliases: []string{"BRIGHT STAR INVESTMENTS", "DERIVE TRADING"}},
	{Name: "Vijay Kedia", Aliases: []string{"KEDIA SECURITIES"}},
	{Name: "Rakesh Jhunjhunwala", Aliases: []string{"RARE ENTERPRISES", "REKHA JHUNJHUNWALA"}},
	{Name: "Anil Kumar Goel", Aliases: []string{"ANIL KUMAR FAMILY TRUST"}},
	{Name: "Mukul Agrawal", Aliases: []string{"PARAM CAPITAL"}},
	{Name: "Dolly Khanna"},
	{Name: "Ashish Kacholia"},
	{Name: "Ashish Dhawan"},
	{Name: "Sunil Singhania", Aliases: []string{"ABAKKUS"}},
	{Name: "Mohnish Pabrai", Aliases: []string{"PABRAI INVESTMENT"}},
	{Name: "Porinju Veliyath", Aliases: []string{"EQUITY INTELLIGENCE"}},
	{Name: "Ramesh Damani"},
	{Name: "Raamdeo Agrawal"},
	{Name: "Akash Bhansali"},
}

var multiSpace = regexp.MustCompile(`\s+`)
var trailingPunct = regexp.MustCompile(`[.\s]+$`)

// normalize collapses a name for comparison: uppercased, trimmed, internal
// runs of whitespace collapsed to one space, trailing punctuation stripped —
// same convention as promoter.normalizePersonKey / deals.normalizeFundName.
func normalize(s string) string {
	s = trailingPunct.ReplaceAllString(strings.ToUpper(strings.TrimSpace(s)), "")
	return multiSpace.ReplaceAllString(s, " ")
}

// match returns the tracked Investor a raw NSE shareholder name belongs to,
// or nil. Exact match on the person's own normalized name; aliases match as
// a substring (see Investor.Aliases doc).
func match(shareholderName string) *Investor {
	n := normalize(shareholderName)
	if n == "" {
		return nil
	}
	for i := range Tracked {
		t := &Tracked[i]
		if normalize(t.Name) == n {
			return t
		}
		for _, a := range t.Aliases {
			if strings.Contains(n, normalize(a)) {
				return t
			}
		}
	}
	return nil
}

// Holding is one tracked investor's latest disclosed position in one stock.
type Holding struct {
	InvestorName  string    `json:"investor_name"`
	Symbol        string    `json:"symbol"`
	CompanyName   string    `json:"company_name"`
	Shares        int64     `json:"shares"`
	PctHolding    float64   `json:"pct_holding"` // percentage, e.g. 1.83 (not 0.0183)
	ReportDate    time.Time `json:"report_date"`
	FirstSeenDate time.Time `json:"first_seen_date"`
}

// InvestorSummary is one card in the list view. TopSymbol/TopPct are the
// investor's single largest disclosed stake — the one concrete, interesting
// fact a summary card can show without opening the detail view.
type InvestorSummary struct {
	InvestorName string    `json:"investor_name"`
	StockCount   int       `json:"stock_count"`
	LatestDate   time.Time `json:"latest_date"`
	TopSymbol    string    `json:"top_symbol"`
	TopPct       float64   `json:"top_pct"`
}
