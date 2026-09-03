package investors

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client fetches NSE's quarterly shareholding-pattern (SHP) filing list and
// per-filing XBRL detail. Like the PIT and bulk/block-deals feeds, this is
// served over plain HTTP/1.1 with a browser User-Agent and no cookie session
// required (verified live — a cookie-less request returns the same 200).
type Client struct {
	http        *http.Client
	listBaseURL string // overridable for tests; empty → live endpoint
}

func NewClient() *Client {
	tr := &http.Transport{
		ForceAttemptHTTP2: false,
		TLSClientConfig:   &tls.Config{NextProtos: []string{"http/1.1"}},
	}
	return &Client{http: &http.Client{Timeout: 25 * time.Second, Transport: tr}}
}

const (
	liveListBase = "https://www.nseindia.com/api/corporate-share-holdings-master"
	userAgent    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// FilingMeta is one row of the SHP filing list — metadata only, the actual
// named shareholders live in the linked XBRL document.
type FilingMeta struct {
	RecordID    string
	Symbol      string
	CompanyName string
	ISIN        string
	ReportDate  time.Time // quarter-end the filing reports as of
	XBRLURL     string
}

type filingRow struct {
	RecordID string `json:"recordId"`
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	ISIN     string `json:"isin"`
	Date     string `json:"date"` // period end, e.g. "21-AUG-2026"
	XBRL     string `json:"xbrl"`
}

func (c *Client) listURL(from, to time.Time) string {
	base := c.listBaseURL
	if base == "" {
		base = liveListBase
	}
	return fmt.Sprintf("%s?index=equities&from_date=%s&to_date=%s",
		base, from.Format("02-01-2006"), to.Format("02-01-2006"))
}

// FetchFilings pulls the SHP filing list for the given date range (inclusive).
func (c *Client) FetchFilings(ctx context.Context, from, to time.Time) ([]FilingMeta, error) {
	body, err := c.get(ctx, c.listURL(from, to), "application/json, text/plain, */*")
	if err != nil {
		return nil, fmt.Errorf("investors: fetch filings: %w", err)
	}
	var rows []filingRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("investors: decode filings: %w", err)
	}
	out := make([]FilingMeta, 0, len(rows))
	for _, row := range rows {
		// NSE occasionally lists a filing with a placeholder xbrl link (e.g.
		// ".../corporate/xbrl/-" — verified live, no real filename) that 404s
		// on every fetch. Skipping those here (before they're ever queued for
		// a detail fetch) is the only way to filter them: FetchDetail can't
		// tell a permanently-broken link from a transient failure by status
		// code alone, so a queued one would 404 and retry forever.
		if row.RecordID == "" || !strings.HasSuffix(strings.ToLower(row.XBRL), ".xml") {
			continue
		}
		reportDate, _ := time.Parse("02-Jan-2006", row.Date)
		out = append(out, FilingMeta{
			RecordID:    row.RecordID,
			Symbol:      row.Symbol,
			CompanyName: strings.TrimSpace(row.Name),
			ISIN:        row.ISIN,
			ReportDate:  reportDate,
			XBRLURL:     row.XBRL,
		})
	}
	return out, nil
}

// ShareholderHolding is one named shareholder's disclosed position, parsed
// from the filing's Individuals/HUF or Other-Indian-Shareholders category
// (the only two categories NSE names individuals/entities in — mutual
// funds/FPIs/etc. are reported as pooled totals, not by name).
type ShareholderHolding struct {
	Name       string
	Shares     int64
	PctHolding float64 // percentage, e.g. 1.83 (already *100 from the raw fraction)
}

// Detail is the parsed content of one SHP XBRL filing.
type Detail struct {
	Symbol       string
	CompanyName  string
	ISIN         string
	ReportDate   time.Time
	Shareholders []ShareholderHolding
}

// xbrlDoc captures every fact element generically — same approach as
// promoter.xbrlDoc: group flat facts by contextRef instead of modeling the
// (much larger) SHP schema.
type xbrlDoc struct {
	XMLName xml.Name `xml:"xbrl"`
	Facts   []fact   `xml:",any"`
}

type fact struct {
	XMLName    xml.Name
	ContextRef string `xml:"contextRef,attr"`
	Value      string `xml:",chardata"`
}

// FetchDetail downloads and parses one filing's XBRL document. Named
// shareholders live in a pair of contexts sharing an id suffix: a "D_"
// prefixed detail context holds NameOfTheShareholder, and the same-suffixed
// context without the prefix holds the paired numeric facts (verified
// live against a real NSE SHP filing — e.g. "D_IndividualsOrHUF_Context15"
// for the name, "IndividualsOrHUF_Context15" for NumberOfShares /
// ShareholdingAsAPercentageOfTotalNumberOfShares).
func (c *Client) FetchDetail(ctx context.Context, xmlURL string) (Detail, error) {
	body, err := c.get(ctx, xmlURL, "application/xml, text/xml, */*")
	if err != nil {
		return Detail{}, fmt.Errorf("investors: fetch detail: %w", err)
	}
	var doc xbrlDoc
	if err := xml.Unmarshal(body, &doc); err != nil {
		return Detail{}, fmt.Errorf("investors: decode detail: %w", err)
	}

	byContext := make(map[string]map[string]string)
	for _, f := range doc.Facts {
		if f.ContextRef == "" {
			continue
		}
		m, ok := byContext[f.ContextRef]
		if !ok {
			m = make(map[string]string)
			byContext[f.ContextRef] = m
		}
		m[f.XMLName.Local] = f.Value
	}

	main := byContext["MainD"]
	d := Detail{
		Symbol:      main["Symbol"],
		CompanyName: strings.TrimSpace(main["NameOfTheCompany"]),
		ISIN:        main["ISIN"],
	}
	if instant := byContext["MainI"]; instant != nil {
		d.ReportDate = parseDate(instant["DateOfReport"])
	}

	for ctxRef, m := range byContext {
		name, ok := m["NameOfTheShareholder"]
		if !ok || !strings.HasPrefix(ctxRef, "D_") {
			continue
		}
		numeric := byContext[strings.TrimPrefix(ctxRef, "D_")]
		if numeric == nil {
			continue
		}
		d.Shareholders = append(d.Shareholders, ShareholderHolding{
			Name:       strings.TrimSpace(name),
			Shares:     parseInt(numeric["NumberOfShares"]),
			PctHolding: parseFloat(numeric["ShareholdingAsAPercentageOfTotalNumberOfShares"]) * 100,
		})
	}
	return d, nil
}

// get performs one GET, retrying once after a short backoff on a
// transient-looking failure — same pattern as promoter/deals clients.
func (c *Client) get(ctx context.Context, url, accept string) ([]byte, error) {
	body, err := c.doGet(ctx, url, accept)
	if err == nil {
		return body, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(1500 * time.Millisecond):
	}
	return c.doGet(ctx, url, accept)
}

func (c *Client) doGet(ctx context.Context, url, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", accept)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body, nil
}

func parseInt(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

func parseDate(s string) time.Time {
	t, _ := time.Parse("2006-01-02", strings.TrimSpace(s))
	return t
}
