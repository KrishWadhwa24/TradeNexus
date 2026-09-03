package deals

import (
	"context"
	"crypto/tls"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client fetches NSE bulk/block deals from the historical CSV export. The JSON
// variant of this endpoint is hard-capped at 70 rows; the CSV export
// (&csv=true) returns the complete day. Like the PIT feed, NSE serves this over
// plain HTTP/1.1 with a browser User-Agent and no cookie session required.
type Client struct {
	http    *http.Client
	baseURL string // overridable for tests; empty → live endpoint
}

// NewClient builds a bulk/block deals client.
func NewClient() *Client {
	tr := &http.Transport{
		ForceAttemptHTTP2: false,
		TLSClientConfig:   &tls.Config{NextProtos: []string{"http/1.1"}},
	}
	return &Client{http: &http.Client{Timeout: 25 * time.Second, Transport: tr}}
}

const (
	liveBase  = "https://www.nseindia.com/api/historicalOR/bulk-block-short-deals"
	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// optionType maps a deal type to the NSE query param value.
func optionType(t Type) string {
	if t == Block {
		return "block_deals"
	}
	return "bulk_deals"
}

func (c *Client) url(t Type, from, to time.Time) string {
	base := c.baseURL
	if base == "" {
		base = liveBase
	}
	return fmt.Sprintf("%s?optionType=%s&from=%s&to=%s&csv=true",
		base, optionType(t), from.Format("02-01-2006"), to.Format("02-01-2006"))
}

// Fetch downloads and parses the deals of the given type for [from, to]
// (inclusive). The CSV feed accepts a real date range, so a wide backfill is a
// single call.
func (c *Client) Fetch(ctx context.Context, t Type, from, to time.Time) ([]Row, error) {
	body, err := c.get(ctx, c.url(t, from, to))
	if err != nil {
		return nil, fmt.Errorf("deals: fetch %s: %w", t, err)
	}
	rows, err := parseCSV(t, body)
	if err != nil {
		return nil, fmt.Errorf("deals: parse %s: %w", t, err)
	}
	return rows, nil
}

// parseCSV parses NSE's deals CSV. Columns are positional (their headers carry
// trailing spaces), in this order:
//
//	Date, Symbol, Security Name, Client Name, Buy/Sell, Quantity, Price, Remarks
func parseCSV(t Type, body []byte) ([]Row, error) {
	// Strip a UTF-8 BOM if present so the first header cell parses cleanly.
	body = trimBOM(body)
	r := csv.NewReader(strings.NewReader(string(body)))
	r.FieldsPerRecord = -1 // tolerate ragged rows rather than hard-failing
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	// records[0] is the header row.
	out := make([]Row, 0, len(records)-1)
	for _, rec := range records[1:] {
		if len(rec) < 7 {
			continue
		}
		date := parseDate(rec[0])
		if date.IsZero() {
			continue // skip malformed / footer rows
		}
		side := strings.ToUpper(strings.TrimSpace(rec[4]))
		if side != "BUY" && side != "SELL" {
			continue
		}
		remarks := ""
		if len(rec) >= 8 {
			remarks = strings.TrimSpace(rec[7])
		}
		out = append(out, Row{
			Type:         t,
			Date:         date,
			Symbol:       strings.TrimSpace(rec[1]),
			SecurityName: strings.TrimSpace(rec[2]),
			ClientName:   strings.TrimSpace(rec[3]),
			Side:         side,
			Quantity:     parseInt(rec[5]),
			Price:        parseFloat(rec[6]),
			Remarks:      remarks,
		})
	}
	return out, nil
}

func (c *Client) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/csv, */*")
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

func trimBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

// parseDate reads NSE's "24-JUL-2026" date. Go's month lookup is case-
// insensitive, but we title-case defensively before parsing.
func parseDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse("02-Jan-2006", titleMonth(s))
	if err != nil {
		return time.Time{}
	}
	return t
}

// titleMonth normalizes "24-JUL-2026" → "24-Jul-2026".
func titleMonth(s string) string {
	parts := strings.Split(s, "-")
	if len(parts) == 3 && len(parts[1]) >= 1 {
		m := strings.ToLower(parts[1])
		parts[1] = strings.ToUpper(m[:1]) + m[1:]
	}
	return strings.Join(parts, "-")
}

func parseInt(s string) int64 {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	var n int64
	_, _ = fmt.Sscanf(s, "%d", &n)
	return n
}

func parseFloat(s string) float64 {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	var f float64
	_, _ = fmt.Sscanf(s, "%f", &f)
	return f
}
