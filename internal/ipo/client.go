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
	SignaledAt   *time.Time `json:"signaled_at"` // when SignalTier was last set (from DB)

	// Subscription breakdown (times subscribed), from the InvestorGain
	// subscription report. Zero until the IPO opens for bidding.
	QIB               float64 `json:"qib"`
	SHNI              float64 `json:"shni"`
	BHNI              float64 `json:"bhni"`
	NII               float64 `json:"nii"`
	RII               float64 `json:"rii"`
	TotalSubscription float64 `json:"total_subscription"`
	AnchorPositive    bool    `json:"anchor_positive"`
}

// Subscription is one IPO's subscription snapshot (times subscribed per
// investor category), parsed from the InvestorGain subscription report.
type Subscription struct {
	QIB            float64
	SHNI           float64
	BHNI           float64
	NII            float64
	RII            float64
	Total          float64
	AnchorPositive bool
}

// feedResponse decodes rows as generic maps. We look fields up by their exact
// JSON key — several keys ("Price (₹)", "IPO Size", "~P/E", …) contain spaces
// or symbols that Go struct tags can't reliably represent (a currency symbol in
// a tag is silently dropped), so map lookup is the safe way to read them.
type feedResponse struct {
	ReportTableData []map[string]any `json:"reportTableData"`
}

// getStr reads a string-ish value by key (numbers are stringified).
func getStr(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	}
	return ""
}

// getInt64 reads an integer id by key (JSON numbers decode as float64).
func getInt64(m map[string]any, key string) int64 {
	switch t := m[key].(type) {
	case float64:
		return int64(t)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return n
	}
	return 0
}

// Client fetches IPO data from InvestorGain.
type Client struct {
	http       *http.Client
	baseURL    string // overridable for tests; empty → live GMP-feed endpoint
	subBaseURL string // overridable for tests; empty → live subscription-feed endpoint
}

// NewClient builds an IPO feed client.
func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 20 * time.Second}}
}

const (
	liveBase = "https://webnodejs.investorgain.com/cloud/v2/report/data-read/331/1"
	// subscriptionLiveBase is InvestorGain's per-category subscription report
	// (QIB/SHNI/BHNI/NII/RII/Total) — same report family as liveBase, different
	// report id.
	subscriptionLiveBase = "https://webnodejs.investorgain.com/cloud/v2/report/data-read/333/1"
)

// financialYear returns the Indian FY label (Apr–Mar), e.g. "2026-27".
func financialYear(t time.Time) string {
	y := t.Year()
	if int(t.Month()) >= 4 {
		return fmt.Sprintf("%d-%02d", y, (y+1)%100)
	}
	return fmt.Sprintf("%d-%02d", y-1, y%100)
}

// url builds the month-scoped GMP report URL for time t.
func (c *Client) url(t time.Time) string {
	if c.baseURL != "" {
		return c.baseURL
	}
	return fmt.Sprintf("%s/%d/%d/%s/0/all?search=&v=%s",
		liveBase, int(t.Month()), t.Year(), financialYear(t), t.Format("15-04"))
}

// subscriptionURL builds the month-scoped subscription report URL for time t.
func (c *Client) subscriptionURL(t time.Time) string {
	if c.subBaseURL != "" {
		return c.subBaseURL
	}
	return fmt.Sprintf("%s/%d/%d/%s/0/all?search=&v=%s",
		subscriptionLiveBase, int(t.Month()), t.Year(), financialYear(t), t.Format("15-04"))
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

// FetchSubscriptions pulls the current month's subscription report and
// returns each IPO's snapshot keyed by its InvestorGain id (the same `~id`
// used by Fetch's IPO.ID, so callers can merge the two by id).
func (c *Client) FetchSubscriptions(ctx context.Context, now time.Time) (map[int64]Subscription, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.subscriptionURL(now), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://www.investorgain.com/")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ipo subscription fetch: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ipo subscription fetch HTTP %d", resp.StatusCode)
	}
	return ParseSubscriptionFeed(body)
}

// ParseSubscriptionFeed decodes the subscription report body into a
// per-IPO-id map (exported for tests).
func ParseSubscriptionFeed(body []byte) (map[int64]Subscription, error) {
	var fr feedResponse
	if err := json.Unmarshal(body, &fr); err != nil {
		return nil, fmt.Errorf("ipo subscription decode: %w", err)
	}
	out := make(map[int64]Subscription, len(fr.ReportTableData))
	for _, m := range fr.ReportTableData {
		id := getInt64(m, "~id")
		if id == 0 {
			continue
		}
		out[id] = Subscription{
			QIB:            parseFloat(getStr(m, "QIB")),
			SHNI:           parseFloat(getStr(m, "SHNI")),
			BHNI:           parseFloat(getStr(m, "BHNI")),
			NII:            parseFloat(getStr(m, "NII")),
			RII:            parseFloat(getStr(m, "RII")),
			Total:          firstBoldFloat(getStr(m, "Total")),
			AnchorPositive: strings.Contains(getStr(m, "Anchor"), "✅"),
		}
	}
	return out, nil
}

// ParseFeed decodes the feed body into cleaned IPO records (exported for tests).
func ParseFeed(body []byte) ([]IPO, error) {
	var fr feedResponse
	if err := json.Unmarshal(body, &fr); err != nil {
		return nil, fmt.Errorf("ipo decode: %w", err)
	}
	out := make([]IPO, 0, len(fr.ReportTableData))
	for _, m := range fr.ReportTableData {
		out = append(out, cleanRow(m))
	}
	return out, nil
}

var (
	reBold      = regexp.MustCompile(`<b>(.*?)</b>`)
	reBoard     = regexp.MustCompile(`bg-secondary[^>]*>([^<]+)</span>`)
	reFire      = regexp.MustCompile(`&#128293;`)
	reStatusLet = regexp.MustCompile(`ms-2">(CT|[UOC])</span>`)
)

func cleanRow(m map[string]any) IPO {
	nameHTML := getStr(m, "Name")
	ipo := IPO{
		ID:           getInt64(m, "~id"),
		Name:         strings.TrimSpace(getStr(m, "~ipo_name")),
		Category:     strings.TrimSpace(getStr(m, "~IPO_Category")),
		Subscription: strings.TrimSpace(getStr(m, "Sub")),
		Price:        strings.TrimSpace(cleanRupee(getStr(m, "Price (₹)"))),
		Size:         cleanRupee(getStr(m, "IPO Size")),
		Lot:          strings.TrimSpace(getStr(m, "Lot")),
		PE:           strings.TrimSpace(getStr(m, "~P/E")),
		URL:          strings.TrimSpace(getStr(m, "~urlrewrite_folder_name")),
		UpdatedOn:    stripTags(getStr(m, "Updated-On")),
		Status:       statusFromName(nameHTML),
		Board:        boardFromName(nameHTML),
		Rating:       len(reFire.FindAllString(getStr(m, "Rating"), -1)),
		GMPPercent:   parseFloat(getStr(m, "~gmp_percent_calc")),
		GMP:          gmpValue(getStr(m, "GMP")),
	}
	ipo.OpenDate = parseDate(getStr(m, "~Srt_Open"))
	ipo.CloseDate = parseDate(getStr(m, "~Srt_Close"))
	ipo.BoADate = parseDate(getStr(m, "~Srt_BoA_Dt"))
	ipo.ListingDate = parseDate(getStr(m, "~Str_Listing"))
	return ipo
}

// statusFromName derives lifecycle from the badges in the Name HTML.
// Listed rows show "L@<price>"; otherwise a badge U/O/C/CT. "CT" (closing
// today) is InvestorGain's marker for an IPO on its last bidding day — still
// open, so it maps to "open" rather than "closed".
func statusFromName(nameHTML string) string {
	if strings.Contains(nameHTML, "L@") {
		return "listed"
	}
	if m := reStatusLet.FindStringSubmatch(nameHTML); m != nil {
		switch m[1] {
		case "U":
			return "upcoming"
		case "O", "CT":
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
	return firstBoldFloat(gmpHTML)
}

// firstBoldFloat parses the number inside the first <b>..</b> in an HTML
// snippet — the InvestorGain feeds bury the actual value (GMP, total
// subscription, …) inside bold tags alongside unrelated markup/labels.
func firstBoldFloat(html string) float64 {
	if m := reBold.FindStringSubmatch(html); m != nil {
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
