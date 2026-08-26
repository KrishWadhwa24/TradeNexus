package insights

import (
	"sort"
	"strings"
	"time"

	"tradenexus/internal/market"
)

// bareSymbol strips an instrument's segment suffix (e.g. "RELIANCE-EQ" →
// "RELIANCE") so it matches the raw NSE symbols used by promoter_trades and
// market_deals. Only a known trailing segment is removed, so hyphenated NSE
// names like "BAJAJ-AUTO" survive.
func bareSymbol(tradingSymbol string) string {
	s := strings.ToUpper(strings.TrimSpace(tradingSymbol))
	for _, suf := range []string{"-EQ", "-BE", "-BZ", "-BL", "-SM", "-ST"} {
		if strings.HasSuffix(s, suf) {
			return strings.TrimSuffix(s, suf)
		}
	}
	return s
}

// perfLabel is a human name for a scanner+timeframe.
func perfLabel(source, timeframe string) string {
	name := source
	switch source {
	case "pine":
		name = "Pine Momentum"
	case "weekly":
		name = "Weekly Confluence"
	case "patterns":
		name = "Chart Pattern"
	}
	if timeframe != "" {
		return name + " · " + timeframe
	}
	return name
}

// sourcesOf lists the human labels of the bullish sources set on a stock.
func sourcesOf(c *ConfluenceStock) []string {
	var out []string
	if c.ScannerBuy {
		out = append(out, "Scanner BUY")
	}
	if c.PromoterBuy {
		out = append(out, "Promoter buy")
	}
	if c.BulkBuy {
		out = append(out, "Bulk net-buy")
	}
	if c.BlockBuy {
		out = append(out, "Block net-buy")
	}
	return out
}

// sortByScore orders the board by score desc, then symbol for stable ties.
func sortByScore(list []ConfluenceStock) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].Score != list[j].Score {
			return list[i].Score > list[j].Score
		}
		return list[i].Symbol < list[j].Symbol
	})
}

func dateOnly(t time.Time) time.Time {
	t = t.In(market.IST)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, market.IST)
}
