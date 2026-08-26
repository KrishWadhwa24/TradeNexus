// Package fiidii tracks NSE's daily FII/DII cash-market activity (buy/sell/net,
// in ₹ crores) and alerts on it once it's published for the day.
package fiidii

import "time"

// Flow is one category's (DII or FII) buy/sell/net value for a trading day,
// in ₹ crores, as published by NSE.
type Flow struct {
	Category  string  `json:"category"`
	BuyValue  float64 `json:"buy_value"`
	SellValue float64 `json:"sell_value"`
	NetValue  float64 `json:"net_value"`
}

// Snapshot is the most recently fetched DII/FII flows for one trade date.
// Date is NSE's own label (e.g. "24-Jul-2026") — on weekends/holidays it's the
// last trading day's data, which is expected and fine to display as-is.
type Snapshot struct {
	Date      string    `json:"date"`
	DII       Flow      `json:"dii"`
	FII       Flow      `json:"fii"`
	FetchedAt time.Time `json:"fetched_at"`
}
