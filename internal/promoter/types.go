// Package promoter tracks SEBI PIT (insider trading) disclosures from NSE and
// surfaces promoter/director/KMP market buys and sells — signal, not noise:
// pledges, ESOPs, gifts, off-market transfers and buybacks are ignored.
package promoter

import (
	"strings"
	"time"
)

// Event types we track. Everything else (pledges, gifts, ESOPs, off-market,
// buybacks, Designated Person / Employee / Trust / Connected Person category)
// is deliberately ignored.
const (
	EventPromoterBuy  = "promoter_buy"
	EventPromoterSell = "promoter_sell"
	EventKMPBuy       = "kmp_buy"
	EventKMPSell      = "kmp_sell"
)

// Trade is one tracked promoter/director/KMP market transaction, parsed from
// a single <Disclosure> block inside an NSE PIT XBRL filing.
type Trade struct {
	ID          string     `json:"id"` // "{appId}:{contextRef}" — unique per disclosure block
	AppID       int64      `json:"app_id"`
	Symbol      string     `json:"symbol"`
	CompanyName string     `json:"company_name"`
	ISIN        string     `json:"isin"`
	PersonName  string     `json:"person_name"`
	Category    string     `json:"category"`   // raw NSE category, e.g. "Promoter Group", "Director"
	EventType   string     `json:"event_type"` // one of the Event* constants
	Mode        string     `json:"mode"`       // "Market Purchase" | "Market Sale"
	Quantity    int64      `json:"quantity"`
	Value       float64    `json:"value_inr"`
	QtyBefore   int64      `json:"qty_before"`
	PctBefore   float64    `json:"pct_before"` // percentage, e.g. 14.67 (not 0.1467)
	QtyAfter    int64      `json:"qty_after"`
	PctAfter    float64    `json:"pct_after"`
	TradeFrom   *time.Time `json:"trade_date_from"`
	TradeTo     *time.Time `json:"trade_date_to"`
	Regulation  string     `json:"regulation"`
	FilingURL   string     `json:"filing_url"` // human-readable iXBRL viewer link
	BroadcastAt time.Time  `json:"broadcast_at"`
	Alerted     bool       `json:"alerted"`
	AlertedAt   *time.Time `json:"alerted_at"`
}

// categoryBucket classifies a raw NSE CategoryOfPerson into "promoter",
// "kmp", or "" (untracked). "Promoter and Director" counts as promoter.
func categoryBucket(category string) string {
	switch {
	case strings.Contains(category, "Promoter"):
		return "promoter"
	case category == "Director" || category == "KMP":
		return "kmp"
	default:
		return ""
	}
}

// classify maps a disclosure's category + mode + transaction type to a
// tracked event type, or "" if it should be ignored. Mode and transaction
// type are cross-checked against each other — a mismatch (which shouldn't
// happen for genuine market trades) is treated as untracked rather than
// guessed at.
func classify(category, mode, txType string) string {
	bucket := categoryBucket(category)
	if bucket == "" {
		return ""
	}
	switch {
	case mode == "Market Purchase" && txType == "Buy":
		return bucket + "_buy"
	case mode == "Market Sale" && txType == "Sell":
		return bucket + "_sell"
	default:
		return ""
	}
}
