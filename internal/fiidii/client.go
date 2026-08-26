package fiidii

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"time"
)

const (
	apiURL     = "https://www.nseindia.com/api/fiidiiTradeReact"
	refererURL = "https://www.nseindia.com/reports/fii-dii"
	userAgent  = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

// rawRow is one row of NSE's fiidiiTradeReact response — a flat 2-element
// array, one row per category ("DII", "FII/FPI").
type rawRow struct {
	Category  string `json:"category"`
	Date      string `json:"date"`
	BuyValue  string `json:"buyValue"`
	SellValue string `json:"sellValue"`
	NetValue  string `json:"netValue"`
}

// Client fetches FII/DII trade data from NSE. NSE fronts this endpoint with
// Akamai bot protection that can block a bare request from some networks, so
// every fetch first visits the report's referer page to pick up a fresh
// session in the cookie jar, then reuses it for the actual API call.
type Client struct {
	http *http.Client
}

// NewClient builds a client with its own cookie jar. NSE's session cookies
// are short-lived, so we re-handshake before every fetch rather than trying
// to persist/reuse cookies across a long-running process.
func NewClient() *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{http: &http.Client{Timeout: 20 * time.Second, Jar: jar}}
}

// Fetch performs the cookie handshake, then downloads and parses the
// currently published DII/FII flows (today's, or the last trading day's on a
// weekend/holiday).
func (c *Client) Fetch(ctx context.Context) (Snapshot, error) {
	if err := c.handshake(ctx); err != nil {
		return Snapshot{}, fmt.Errorf("fiidii handshake: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return Snapshot{}, err
	}
	setCommonHeaders(req)
	req.Header.Set("Accept", "*/*")

	resp, err := c.http.Do(req)
	if err != nil {
		return Snapshot{}, fmt.Errorf("fiidii fetch: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Snapshot{}, fmt.Errorf("fiidii fetch HTTP %d", resp.StatusCode)
	}

	var rows []rawRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return Snapshot{}, fmt.Errorf("fiidii decode: %w", err)
	}
	return parseRows(rows)
}

// handshake visits the report's referer page so NSE's bot-manager sets
// session cookies in the client's jar before the real API call.
func (c *Client) handshake(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, refererURL, nil)
	if err != nil {
		return err
	}
	setCommonHeaders(req)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func setCommonHeaders(req *http.Request) {
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", refererURL)
}

func parseRows(rows []rawRow) (Snapshot, error) {
	var snap Snapshot
	for _, r := range rows {
		f := Flow{
			Category:  r.Category,
			BuyValue:  parseFloat(r.BuyValue),
			SellValue: parseFloat(r.SellValue),
			NetValue:  parseFloat(r.NetValue),
		}
		if snap.Date == "" {
			snap.Date = r.Date
		}
		switch r.Category {
		case "DII":
			snap.DII = f
		case "FII/FPI", "FII":
			snap.FII = f
		}
	}
	if snap.Date == "" {
		return Snapshot{}, fmt.Errorf("fiidii: empty or unrecognized response")
	}
	snap.FetchedAt = time.Now()
	return snap, nil
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
