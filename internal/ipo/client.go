// Package ipo tracks open + upcoming IPOs and their grey-market premium (GMP)
// from the InvestorGain feed, and fires "apply" signals on the last bidding day.
package ipo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// IPO is one cleaned, parsed IPO record.
type IPO struct {
	ID           int64      `json:"id"`
	Name         string     `json:"name"`
	Board        string     `json:"board"`    // IPO | BSE SME | NSE SME
	Category     string     `json:"category"` // IPO | SME
	Status       string     `json:"status"`   // open | upcoming
	GMP          float64    `json:"gmp"`
	GMPPercent   float64    `json:"gmp_percent"`
	Subscription string     `json:"subscription"`
	Price        string     `json:"price"`
	Size         string     `json:"ipo_size"`
	Lot          string     `json:"lot"`
	PE           string     `json:"pe"`
	Rating       int        `json:"rating"`
	OpenDate     *time.Time `json:"open_date"`
	CloseDate    *time.Time `json:"close_date"`
	BoADate      *time.Time `json:"boa_date"`
	ListingDate  *time.Time `json:"listing_date"`
	URL          string     `json:"url"`
	UpdatedOn    string     `json:"updated_on"`
	SignalTier   string     `json:"signal_tier"` // '', your_choice, apply, admin_apply (from DB)
}

// rawRow mirrors the (HTML-laden) JSON objects the feed returns. Only the fields
// we use are declared; the rest are ignored.
type rawRow struct {
	ID          int64  `json:"~id"`
	IPOName     string `json:"~ipo_name"`
	Category    string `json:"~IPO_Category"`
	GMPPctCalc  string `json:"~gmp_percent_calc"`
	NameHTML    string `json:"Name"`
	GMPHTML     string `json:"GMP"`
	Rating      string `json:"Rating"`
	Sub         string `json:"Sub"`
	Price       string `json:"Price (₹)"`
	Size        string `json:"IPO Size"`
	Lot         string `json:"Lot"`
	PE          string `json:"~P/E"`
	SrtOpen     string `json:"~Srt_Open"`
	SrtClose    string `json:"~Srt_Close"`
	SrtBoA      string `json:"~Srt_BoA_Dt"`
	StrListing  string `json:"~Str_Listing"`
	URLRewrite  string `json:"~urlrewrite_folder_name"`
	UpdatedHTML string `json:"Updated-On"`
}

type feedResponse struct {
	ReportTableData []rawRow `json:"reportTableData"`
}

// Client fetches IPO data from InvestorGain.
type Client struct {
	http    *http.Client
	baseURL string // overridable for tests; empty → live endpoint
}

// NewClient builds an IPO feed client.
func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 20 * time.Second}}
}

const liveBase = "https://webnodejs.investorgain.com/cloud/v2/report/data-read/331/1"

// financialYear returns the Indian FY label (Apr–Mar), e.g. "2026-27".
func financialYear(t time.Time) string {
	y := t.Year()
	if int(t.Month()) >= 4 {
		return fmt.Sprintf("%d-%02d", y, (y+1)%100)
	}
	return fmt.Sprintf("%d-%02d", y-1, y%100)
}

// url builds the month-scoped report URL for time t.
func (c *Client) url(t time.Time) string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return fmt.Sprintf("%s/%d/%d/%s/0/all?search=&v=%s",
		liveBase, int(t.Month()), t.Year(), financialYear(t), t.Format("15-04"))
}

// Fetch pulls the current month's IPO report and returns cleaned records.
func (c *Client) Fetch(ctx context.Context, now time.Time) ([]IPO, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(now), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://www.investorgain.com/")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ipo fetch: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ipo fetch HTTP %d", resp.StatusCode)
	}
	return ParseFeed(body)
}

// ParseFeed decodes the feed body into cleaned IPO records (exported for tests).
func ParseFeed(body []byte) ([]IPO, error) {
	var fr feedResponse
	if err := json.Unmarshal(body, &fr); err != nil {
		return nil, fmt.Errorf("ipo decode: %w", err)
	}
	out := make([]IPO, 0, len(fr.ReportTableData))
	for _, r := range fr.ReportTableData {
		out = append(out, r.clean())
	}
	return out, nil
}

var (
	reBold      = regexp.MustCompile(`<b>(.*?)</b>`)
	reBoard     = regexp.MustCompile(`bg-secondary[^>]*>([^<]+)</span>`)
	reFire      = regexp.MustCompile(`&#128293;`)
	reStatusLet = regexp.MustCompile(`ms-2">([UOC])</span>`)
)

func (r rawRow) clean() IPO {
	ipo := IPO{
		ID:           r.ID,
		Name:         strings.TrimSpace(r.IPOName),
		Category:     strings.TrimSpace(r.Category),
		Subscription: strings.TrimSpace(r.Sub),
		Price:        strings.TrimSpace(r.Price),
		Size:         cleanRupee(r.Size),
		Lot:          strings.TrimSpace(r.Lot),
		PE:           strings.TrimSpace(r.PE),
		URL:          strings.TrimSpace(r.URLRewrite),
		UpdatedOn:    stripTags(r.UpdatedHTML),
		Status:       statusFromName(r.NameHTML),
		Board:        boardFromName(r.NameHTML),
		Rating:       len(reFire.FindAllString(r.Rating, -1)),
		GMPPercent:   parseFloat(r.GMPPctCalc),
		GMP:          gmpValue(r.GMPHTML),
	}
	ipo.OpenDate = parseDate(r.SrtOpen)
	ipo.CloseDate = parseDate(r.SrtClose)
	ipo.BoADate = parseDate(r.SrtBoA)
	ipo.ListingDate = parseDate(r.StrListing)
	return ipo
}

// statusFromName derives lifecycle from the badges in the Name HTML.
// Listed rows show "L@<price>"; otherwise a single-letter badge U/O/C.
func statusFromName(nameHTML string) string {
	if strings.Contains(nameHTML, "L@") {
		return "listed"
	}
	if m := reStatusLet.FindStringSubmatch(nameHTML); m != nil {
		switch m[1] {
		case "U":
			return "upcoming"
		case "O":
			return "open"
		case "C":
			return "closed"
		}
	}
	return "unknown"
}

// boardFromName extracts the exchange/board label (first bg-secondary badge).
func boardFromName(nameHTML string) string {
	if m := reBoard.FindStringSubmatch(nameHTML); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// gmpValue parses the ₹ premium from the GMP HTML (the <b>..</b> value).
func gmpValue(gmpHTML string) float64 {
	if m := reBold.FindStringSubmatch(gmpHTML); m != nil {
		return parseFloat(m[1])
	}
	return 0
}

func cleanRupee(s string) string {
	s = strings.ReplaceAll(s, "&#8377;", "")
	return strings.TrimSpace(s)
}

func stripTags(s string) string {
	// Remove any HTML tags, keep inner text.
	var b strings.Builder
	depth := 0
	for _, ch := range s {
		switch ch {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(ch)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "--" || s == "-" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
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
