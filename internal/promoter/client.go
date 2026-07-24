package promoter

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

// Client fetches NSE PIT (insider trading) filings and their XBRL detail.
// NSE serves this feed over plain HTTP/1.1 without requiring a cookie
// session — a forced-1.1 transport and a browser User-Agent are enough.
type Client struct {
	http        *http.Client
	listBaseURL string // overridable for tests; empty → live endpoint
}

// NewClient builds a PIT feed client.
func NewClient() *Client {
	tr := &http.Transport{
		ForceAttemptHTTP2: false,
		TLSClientConfig:   &tls.Config{NextProtos: []string{"http/1.1"}},
	}
	return &Client{http: &http.Client{Timeout: 20 * time.Second, Transport: tr}}
}

const (
	liveListBase = "https://www.nseindia.com/api/corporates-pit-gg"
	userAgent    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// FilingMeta is one row of the PIT filing list — metadata only, the actual
// buy/sell detail lives in the linked XBRL document.
type FilingMeta struct {
	AppID       int64
	Symbol      string
	CompanyName string
	Regulation  string
	IXBRL       string // human-readable viewer link
	XMLFileName string // raw XBRL document to parse
	BroadcastAt time.Time
}

type filingListResponse struct {
	Data []filingRow `json:"data"`
}

type filingRow struct {
	AppID             string `json:"appId"`
	BroadcastDateTime string `json:"broadcastDateTime"`
	CompanyName       string `json:"companyName"`
	Symbol            string `json:"symbol"`
	Regulation        string `json:"regulation"`
	IXBRL             string `json:"ixbrl"`
	XMLFileName       string `json:"xmlFileName"`
}

func (c *Client) listURL(from, to time.Time) string {
	base := c.listBaseURL
	if base == "" {
		base = liveListBase
	}
	return fmt.Sprintf("%s?index=equities&from_date=%s&to_date=%s",
		base, from.Format("02-01-2006"), to.Format("02-01-2006"))
}

// FetchFilings pulls the PIT filing list for the given date range (inclusive).
func (c *Client) FetchFilings(ctx context.Context, from, to time.Time) ([]FilingMeta, error) {
	body, err := c.get(ctx, c.listURL(from, to), "application/json, text/plain, */*")
	if err != nil {
		return nil, fmt.Errorf("promoter: fetch filings: %w", err)
	}
	var lr filingListResponse
	if err := json.Unmarshal(body, &lr); err != nil {
		return nil, fmt.Errorf("promoter: decode filings: %w", err)
	}
	out := make([]FilingMeta, 0, len(lr.Data))
	for _, row := range lr.Data {
		appID, err := strconv.ParseInt(strings.TrimSpace(row.AppID), 10, 64)
		if err != nil {
			continue
		}
		broadcastAt, _ := time.ParseInLocation("02-Jan-2006 15:04:05", row.BroadcastDateTime, time.Local)
		out = append(out, FilingMeta{
			AppID:       appID,
			Symbol:      row.Symbol,
			CompanyName: strings.TrimSpace(row.CompanyName),
			Regulation:  row.Regulation,
			IXBRL:       row.IXBRL,
			XMLFileName: row.XMLFileName,
			BroadcastAt: broadcastAt,
		})
	}
	return out, nil
}

// Disclosure is one <Disclosure*> context block within a PIT XBRL filing —
// one person's transaction. A single filing can contain several.
type Disclosure struct {
	ContextRef      string
	Category        string
	PersonName      string
	Mode            string
	TransactionType string
	Quantity        int64
	Value           float64
	QtyBefore       int64
	PctBefore       float64 // percentage, e.g. 14.67
	QtyAfter        int64
	PctAfter        float64
	DateFrom        *time.Time
	DateTo          *time.Time
}

// Detail is the parsed content of one PIT XBRL filing.
type Detail struct {
	Symbol      string
	CompanyName string
	ISIN        string
	Regulation  string
	Disclosures []Disclosure
}

// xbrlDoc captures every fact element generically — the PIT schema has no
// single Go-friendly structure (deeply nested contexts, dozens of optional
// tags), so we group flat facts by their contextRef instead of modeling the
// schema.
type xbrlDoc struct {
	XMLName xml.Name `xml:"xbrl"`
	Facts   []fact   `xml:",any"`
}

type fact struct {
	XMLName    xml.Name
	ContextRef string `xml:"contextRef,attr"`
	Value      string `xml:",chardata"`
}

// FetchDetail downloads and parses one filing's XBRL document.
func (c *Client) FetchDetail(ctx context.Context, xmlURL string) (Detail, error) {
	body, err := c.get(ctx, xmlURL, "application/xml, text/xml, */*")
	if err != nil {
		return Detail{}, fmt.Errorf("promoter: fetch detail: %w", err)
	}
	var doc xbrlDoc
	if err := xml.Unmarshal(body, &doc); err != nil {
		return Detail{}, fmt.Errorf("promoter: decode detail: %w", err)
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

	main := byContext["MainI"]
	d := Detail{
		Symbol:      main["Symbol"],
		CompanyName: strings.TrimSpace(main["NameOfTheCompany"]),
		ISIN:        main["ISINCode"],
		Regulation:  main["DisclosureUnderRegulation"],
	}
	for ctxRef, m := range byContext {
		if ctxRef == "MainI" || m["CategoryOfPerson"] == "" {
			continue // company-level context, or not a person-disclosure block
		}
		d.Disclosures = append(d.Disclosures, Disclosure{
			ContextRef:      ctxRef,
			Category:        strings.TrimSpace(m["CategoryOfPerson"]),
			PersonName:      strings.TrimSpace(m["NameOfThePerson"]),
			Mode:            strings.TrimSpace(m["ModeOfAcquisitionOrDisposal"]),
			TransactionType: strings.TrimSpace(m["SecuritiesAcquiredOrDisposedTransactionType"]),
			Quantity:        parseInt(m["SecuritiesAcquiredOrDisposedNumberOfSecurity"]),
			Value:           parseFloat(m["SecuritiesAcquiredOrDisposedValueOfSecurity"]),
			QtyBefore:       parseInt(m["SecuritiesHeldPriorToAcquisitionOrDisposalNumberOfSecurity"]),
			PctBefore:       parseFloat(m["SecuritiesHeldPriorToAcquisitionOrDisposalPercentageOfShareholding"]) * 100,
			QtyAfter:        parseInt(m["SecuritiesHeldPostAcquistionOrDisposalNumberOfSecurity"]),
			PctAfter:        parseFloat(m["SecuritiesHeldPostAcquistionOrDisposalPercentageOfShareholding"]) * 100,
			DateFrom:        parseDate(m["DateOfAllotmentAdviceOrAcquisitionOfSharesOrSaleOfSharesSpecifyFromDate"]),
			DateTo:          parseDate(m["DateOfAllotmentAdviceOrAcquisitionOfSharesOrSaleOfSharesSpecifyToDate"]),
		})
	}
	return d, nil
}

// get performs one GET, retrying once after a short backoff on a
// transient-looking failure (NSE occasionally blips under load).
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

func parseDate(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}
