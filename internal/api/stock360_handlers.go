package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"tradenexus/internal/analytics"
	"tradenexus/internal/deals"
	"tradenexus/internal/investors"
	"tradenexus/internal/promoter"
	"tradenexus/internal/signals"
)

// baseSymbol strips the "-EQ" suffix NSE equity instruments carry (e.g.
// "SUVIDHAA-EQ" → "SUVIDHAA") — the promoter/deals/mutual-fund feeds all key
// on the bare company symbol reported by NSE's own disclosure/deal feeds,
// not the tradeable-instrument symbol from the Angel scrip master.
func baseSymbol(tradingSymbol string) string {
	return strings.TrimSuffix(tradingSymbol, "-EQ")
}

// Stock360 is one stock's full picture — pure aggregation over data every
// other section (Promoter Trades, Mutual Fund Analyser, Bulk/Block Deals,
// Scanners) already collects independently. No new data source.
type Stock360 struct {
	InstrumentID int64                     `json:"instrument_id"`
	Symbol       string                    `json:"symbol"`
	CompanyName  string                    `json:"company_name"`
	Price        analytics.Params          `json:"price"`
	Promoters    []promoter.PersonPosition `json:"promoters"`
	Funds        []deals.FundHolder        `json:"funds"`
	BigInvestors []investors.Holding       `json:"big_investors"`
	BulkDeals    deals.StockDetail         `json:"bulk_deals"`
	BlockDeals   deals.StockDetail         `json:"block_deals"`
	Signals      []signals.Signal          `json:"signals"`
}

// GET /v1/stocks/{id}/360
func (s *Server) handleStock360(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid instrument id"})
		return
	}
	inst, err := s.inst.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "instrument not found"})
		return
	}
	ctx := r.Context()
	symbol := baseSymbol(inst.TradingSymbol)
	out := Stock360{InstrumentID: id, Symbol: inst.TradingSymbol, CompanyName: inst.Name}

	if p, err := s.instrumentParamsWithSync(r, id); err == nil {
		out.Price = p
	}
	if s.promoter != nil {
		if detail, err := s.promoter.GetStockBuying(ctx, symbol); err == nil {
			out.Promoters = detail.People
		}
	}
	if s.deals != nil {
		out.Funds, _ = s.deals.GetStockFunds(ctx, symbol)
		if bd, err := s.deals.GetStock(ctx, deals.Bulk, symbol); err == nil {
			out.BulkDeals = bd
		}
		if bd, err := s.deals.GetStock(ctx, deals.Block, symbol); err == nil {
			out.BlockDeals = bd
		}
	}
	if s.investors != nil {
		out.BigInvestors, _ = s.investors.GetStockHoldings(ctx, symbol)
	}
	if s.signals != nil {
		if sigs, err := s.signals.List(ctx, signals.Filter{InstrumentID: &id, Limit: 20}); err == nil {
			out.Signals = sigs
		}
	}
	writeJSON(w, http.StatusOK, out)
}
