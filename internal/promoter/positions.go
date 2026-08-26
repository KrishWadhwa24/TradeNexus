package promoter

import (
	"context"
	"strings"
	"time"
)

// normalizePersonKey collapses a disclosure's free-text person_name into a
// stable grouping key. NSE's filings have no PAN/DIN in this feed — only the
// name string — and it isn't always consistently cased across filings (e.g.
// "ASHISH RAI" vs "Ashish Rai" for the same person), so grouping must ignore
// case/whitespace or the same person would fragment into multiple rows.
func normalizePersonKey(name string) string {
	return strings.ToUpper(strings.TrimSpace(name))
}

// tradeDate is the date a disclosure's stake change is attributed to. TradeTo
// is usually present; BroadcastAt is the fallback for the rare filing missing it.
func tradeDate(t Trade) time.Time {
	if t.TradeTo != nil {
		return *t.TradeTo
	}
	return t.BroadcastAt
}

// StockSummary is one card in the promoter-buying list view: a stock's
// combined promoter/KMP stake change across every person tracked against it,
// ever (not retention-windowed).
type StockSummary struct {
	Symbol        string    `json:"symbol"`
	CompanyName   string    `json:"company_name"`
	PersonCount   int       `json:"person_count"`
	FirstPct      float64   `json:"first_pct"`      // combined stake % at first observation
	LatestPct     float64   `json:"latest_pct"`     // combined stake % now
	PointIncrease float64   `json:"point_increase"` // latest_pct - first_pct
	BuyValue      float64   `json:"buy_value"`
	SellValue     float64   `json:"sell_value"`
	LatestDate    time.Time `json:"latest_date"`
}

// PersonPosition is one person's tracked stake position in one stock.
type PersonPosition struct {
	PersonName          string    `json:"person_name"`
	Category            string    `json:"category"`
	FirstPct            float64   `json:"first_pct"`
	FirstDate           time.Time `json:"first_date"`
	LatestPct           float64   `json:"latest_pct"`
	LatestDate          time.Time `json:"latest_date"`
	PointIncrease       float64   `json:"point_increase"`
	RelativeIncreasePct float64   `json:"relative_increase_pct"` // 0 when first_pct is 0 (undefined)
	BuyQty              int64     `json:"buy_qty"`
	SellQty             int64     `json:"sell_qty"`
	BuyValue            float64   `json:"buy_value"`
	SellValue           float64   `json:"sell_value"`
	DisclosureCount     int       `json:"disclosure_count"`
}

// StockDetail is the modal payload for one stock: every tracked person's
// position, largest point increase first.
type StockDetail struct {
	Symbol string           `json:"symbol"`
	People []PersonPosition `json:"people"`
}

func relativeIncrease(first, latest float64) float64 {
	if first == 0 {
		return 0
	}
	return (latest - first) / first * 100
}

// ListStockBuying returns stock summaries ranked by combined promoter/KMP
// stake point-increase, largest first.
func (s *Service) ListStockBuying(ctx context.Context) ([]StockSummary, error) {
	return s.repo.ListStockPositions(ctx)
}

// GetStockBuying returns every tracked person's position for one symbol.
func (s *Service) GetStockBuying(ctx context.Context, symbol string) (StockDetail, error) {
	people, err := s.repo.PersonPositionsForSymbol(ctx, symbol)
	if err != nil {
		return StockDetail{}, err
	}
	return StockDetail{Symbol: symbol, People: people}, nil
}

// PersonHistory returns one person's individual transaction history for one
// stock — rate/qty/date per disclosure — bound by whatever's still in
// promoter_trades (see Repo.ListForPerson for the retention caveat).
func (s *Service) PersonHistory(ctx context.Context, symbol, personName string) ([]Trade, error) {
	return s.repo.ListForPerson(ctx, symbol, normalizePersonKey(personName))
}

// BackfillPositions seeds promoter_positions from whatever's currently
// stored in promoter_trades — a one-time bootstrap for history that predates
// this feature (and the only way to recover it, since promoter_trades itself
// is pruned after PromoterRetentionDays). Safe to re-run — see
// Repo.BackfillPositions.
func (s *Service) BackfillPositions(ctx context.Context) error {
	return s.repo.BackfillPositions(ctx)
}
