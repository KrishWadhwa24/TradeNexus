package deals

import (
	"context"
	"regexp"
	"strings"
	"time"
)

// Mutual funds aren't tagged by NSE's feed — client_name is free text — but
// "MUTUAL FUND" reliably appears in every AMC's disclosed name (SBI MUTUAL
// FUND, HDFC MUTUAL FUND, ...), with no observed false positives.
func isMutualFund(clientName string) bool {
	return strings.Contains(strings.ToUpper(clientName), "MUTUAL FUND")
}

// trailingPunct strips trailing periods/whitespace so e.g. "HDFC MUTUAL
// FUND" and "HDFC MUTUAL FUND." (both seen in real NSE data) collapse to one
// fund identity instead of splitting into two.
var trailingPunct = regexp.MustCompile(`[.\s]+$`)

func normalizeFundName(clientName string) string {
	return trailingPunct.ReplaceAllString(strings.ToUpper(strings.TrimSpace(clientName)), "")
}

// FundSummary is one card in the mutual-fund list view: one fund's
// all-time (not retention-windowed) position across every stock it has
// ever traded as a bulk/block deal client.
type FundSummary struct {
	FundName      string    `json:"fund_name"`
	StockCount    int       `json:"stock_count"`
	BuyValue      float64   `json:"buy_value"`
	SellValue     float64   `json:"sell_value"`
	NetValue      float64   `json:"net_value"`
	FirstDealDate time.Time `json:"first_deal_date"`
	LastDealDate  time.Time `json:"last_deal_date"`
}

// FundStock is one stock a fund has traded — the row that feeds both the
// bar chart (buy_value/sell_value per stock) and the pie chart (each
// stock's share of the fund's total buy_value).
type FundStock struct {
	Symbol       string    `json:"symbol"`
	SecurityName string    `json:"security_name"`
	BuyQty       int64     `json:"buy_qty"`
	SellQty      int64     `json:"sell_qty"`
	BuyValue     float64   `json:"buy_value"`
	SellValue    float64   `json:"sell_value"`
	NetQty       int64     `json:"net_qty"`
	NetValue     float64   `json:"net_value"`
	DealCount    int       `json:"deal_count"`
	LastDealDate time.Time `json:"last_deal_date"`
}

// FundDetail is the modal payload for one fund.
type FundDetail struct {
	FundName  string      `json:"fund_name"`
	BuyValue  float64     `json:"buy_value"`
	SellValue float64     `json:"sell_value"`
	Stocks    []FundStock `json:"stocks"` // net_value desc
}

// ListFunds returns mutual-fund summary cards, largest gross value first.
func (s *Service) ListFunds(ctx context.Context) ([]FundSummary, error) {
	return s.repo.ListFundPositions(ctx)
}

// GetFund returns one fund's per-stock position (bar/pie chart source).
// An unknown fundName returns an empty Stocks slice, not an error — same
// convention as GetStock for an unknown symbol.
func (s *Service) GetFund(ctx context.Context, fundName string) (FundDetail, error) {
	stocks, err := s.repo.FundPositionStocks(ctx, normalizeFundName(fundName))
	if err != nil {
		return FundDetail{}, err
	}
	d := FundDetail{FundName: normalizeFundName(fundName), Stocks: stocks}
	for _, st := range stocks {
		d.BuyValue += st.BuyValue
		d.SellValue += st.SellValue
	}
	return d, nil
}

// BackfillFundPositions seeds mutual_fund_positions from whatever raw deal
// rows are currently stored in market_deals — a one-time bootstrap for
// history that predates this feature. Safe to call more than once (see
// Repo.BackfillFundPositions). Exposed only via an explicit admin action,
// never called automatically.
func (s *Service) BackfillFundPositions(ctx context.Context) error {
	return s.repo.BackfillFundPositions(ctx)
}
