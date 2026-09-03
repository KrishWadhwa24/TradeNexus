// Package deals tracks NSE bulk and block deals: fetches the daily CSV feed,
// stores each client's buy/sell rows, nets them per client to filter out
// intraday round-trip churn, and surfaces genuine net accumulation/distribution
// as UI cards and Telegram alerts.
package deals

import (
	"sort"
	"time"
)

// Type is a deal category. Bulk deals are single-broker aggregated trades
// crossing 0.5% of listed shares (noisy — dominated by prop/HFT round-trips,
// so we net + threshold them). Block deals are negotiated ≥₹10cr institutional
// trades (already meaningful — no net threshold).
type Type string

const (
	Bulk  Type = "bulk"
	Block Type = "block"
)

// Valid reports whether t is a known deal type.
func (t Type) Valid() bool { return t == Bulk || t == Block }

// Row is one raw NSE deal row: a single client's single side of a deal in one
// stock on one day.
type Row struct {
	Type         Type      `json:"deal_type"`
	Date         time.Time `json:"date"`
	Symbol       string    `json:"symbol"`
	SecurityName string    `json:"security_name"`
	ClientName   string    `json:"client_name"`
	Side         string    `json:"buy_sell"` // "BUY" | "SELL"
	Quantity     int64     `json:"quantity"`
	Price        float64   `json:"price"`
	Remarks      string    `json:"remarks"`
}

// ClientNet is one client's netted position in a stock over some window (a
// single day for alerts, the 30-day window for the UI). NetValue is the signed
// rupee flow (buy value − sell value); its magnitude is what the bulk-deal
// significance threshold is applied to, so pure round-trips (net ≈ 0) drop out.
type ClientNet struct {
	ClientName string  `json:"client_name"`
	BuyQty     int64   `json:"buy_qty"`
	SellQty    int64   `json:"sell_qty"`
	BuyValue   float64 `json:"buy_value"`
	SellValue  float64 `json:"sell_value"`
	NetQty     int64   `json:"net_qty"`   // buy − sell (>0 net buyer, <0 net seller)
	NetValue   float64 `json:"net_value"` // buyValue − sellValue
}

// AvgPrice is a representative price for display (net value per net share).
func (c ClientNet) AvgPrice() float64 {
	if c.NetQty == 0 {
		return 0
	}
	v := c.NetValue / float64(c.NetQty)
	if v < 0 {
		v = -v
	}
	return v
}

// StockSummary is one card in the list view. Stock-level net (Σ across clients)
// is deliberately NOT surfaced — for block deals every buy has a matching sell,
// so it's always ~0. We show buy vs sell side totals and the single biggest net
// participant (the actual signal) instead.
type StockSummary struct {
	Symbol       string    `json:"symbol"`
	SecurityName string    `json:"security_name"`
	LastDealDate time.Time `json:"last_deal_date"`
	BuyValue     float64   `json:"buy_value"`  // Σ buy-side value
	SellValue    float64   `json:"sell_value"` // Σ sell-side value
	TradedQty    int64     `json:"traded_qty"` // max(buyQty, sellQty) — shares that moved
	BuyerCount   int       `json:"buyer_count"`
	SellerCount  int       `json:"seller_count"`
	TopNetClient string    `json:"top_net_client"` // biggest net participant
	TopNetQty    int64     `json:"top_net_qty"`    // its net (>0 buyer, <0 seller)
	TopNetValue  float64   `json:"top_net_value"`
}

// StockDetail is the modal payload: per-client nets (split into buyers/sellers)
// plus the raw rows, over the window.
type StockDetail struct {
	Symbol       string      `json:"symbol"`
	SecurityName string      `json:"security_name"`
	Days         int         `json:"days"`
	BuyValue     float64     `json:"buy_value"`
	SellValue    float64     `json:"sell_value"`
	TradedQty    int64       `json:"traded_qty"`
	NetBuyers    []ClientNet `json:"net_buyers"`  // net_qty > 0, largest net value first
	NetSellers   []ClientNet `json:"net_sellers"` // net_qty < 0, largest magnitude first
	Rows         []Row       `json:"rows"`        // raw, newest first
}

// AuditEntry is one sent alert, for the "which signals went out" audit view.
type AuditEntry struct {
	Symbol       string    `json:"symbol"`
	SecurityName string    `json:"security_name"`
	DealDate     time.Time `json:"deal_date"`
	AlertedAt    time.Time `json:"alerted_at"`
	BuyValue     float64   `json:"buy_value"`
	SellValue    float64   `json:"sell_value"`
	TradedQty    int64     `json:"traded_qty"`
	Price        float64   `json:"price"` // top net participant's price — the stock's price when the signal fired
}

// netByClient aggregates raw rows into one ClientNet per client name, ordered
// by descending net value (net buyers first, net sellers last).
func netByClient(rows []Row) []ClientNet {
	byClient := make(map[string]*ClientNet)
	for _, r := range rows {
		c, ok := byClient[r.ClientName]
		if !ok {
			c = &ClientNet{ClientName: r.ClientName}
			byClient[r.ClientName] = c
		}
		value := float64(r.Quantity) * r.Price
		if r.Side == "BUY" {
			c.BuyQty += r.Quantity
			c.BuyValue += value
		} else {
			c.SellQty += r.Quantity
			c.SellValue += value
		}
	}
	out := make([]ClientNet, 0, len(byClient))
	for _, c := range byClient {
		c.NetQty = c.BuyQty - c.SellQty
		c.NetValue = c.BuyValue - c.SellValue
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NetValue > out[j].NetValue })
	return out
}

// absValue returns |net value|.
func absValue(c ClientNet) float64 {
	if c.NetValue < 0 {
		return -c.NetValue
	}
	return c.NetValue
}

// significant reports whether any client's net value magnitude clears minNet
// (the bulk-deal churn filter). minNet <= 0 disables the filter.
func significant(nets []ClientNet, minNet float64) bool {
	if minNet <= 0 {
		return true
	}
	for _, c := range nets {
		if absValue(c) >= minNet {
			return true
		}
	}
	return false
}

// splitBuyersSellers partitions nets (already sorted desc by net value) into net
// buyers (net_qty > 0) and net sellers (net_qty < 0, largest magnitude first).
// Pure-flat clients (net_qty == 0) are dropped from both.
func splitBuyersSellers(nets []ClientNet) (buyers, sellers []ClientNet) {
	for _, c := range nets {
		if c.NetQty > 0 {
			buyers = append(buyers, c)
		} else if c.NetQty < 0 {
			sellers = append(sellers, c)
		}
	}
	// sellers currently ascending (most negative last, since sorted desc); flip.
	for i, j := 0, len(sellers)-1; i < j; i, j = i+1, j-1 {
		sellers[i], sellers[j] = sellers[j], sellers[i]
	}
	return buyers, sellers
}
