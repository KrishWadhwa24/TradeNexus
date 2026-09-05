package paper

// Statutory + broker charges on an executed options order, per the rate
// card published by Angel One (this platform's own broker) and confirmed
// identical on Zerodha's public charges page, as of FY2026-27:
//
//	brokerage    Rs.20 flat per executed order
//	STT          0.15% of premium turnover, SELL side only
//	transaction  0.03553% of premium turnover (NSE), both sides
//	SEBI         Rs.10 per crore of turnover, both sides
//	IPFT         Rs.0.01 per crore of turnover, both sides
//	stamp duty   0.003% of premium turnover, BUY side only
//	GST          18% on (brokerage + transaction + SEBI + IPFT)
//
// These are deliberately package-level vars, not consts, so a future
// settings screen can override them per user without touching this math —
// but nothing does that today.
var (
	brokeragePerOrder     = 20.0
	sttSellRate           = 0.0015    // 0.15%
	transactionRate       = 0.0003553 // 0.03553%
	sebiRate              = 0.000001  // Rs.10 / crore
	ipftRate              = 0.000000001
	stampDutyBuyRate      = 0.00003 // 0.003%
	gstRate               = 0.18
	incomeTaxEstimateSlab = 0.30 // display-only; see EstimateIncomeTax
)

// Charges is one executed order's costs, itemised so the UI can show where
// the money actually went rather than a single opaque number.
type Charges struct {
	Brokerage   float64 `json:"brokerage"`
	STT         float64 `json:"stt"`
	Transaction float64 `json:"transaction"`
	SEBI        float64 `json:"sebi"`
	IPFT        float64 `json:"ipft"`
	StampDuty   float64 `json:"stamp_duty"`
	GST         float64 `json:"gst"`
	Total       float64 `json:"total"`
}

// OptionCharges computes the real-world cost of one executed options order.
// side is SideBuy or SideSell; premium is the per-unit option price and qty
// the number of units (NOT lots — turnover is premium*qty either way).
//
// Options only. Equity cash-segment trades run a different rate card
// (different STT rates, different stamp duty, brokerage-free delivery on
// most brokers) which is NOT modelled here — callers must only apply this
// to instruments where OptionType != "". See chargesFor.
func OptionCharges(side string, premium float64, qty int) Charges {
	turnover := premium * float64(qty)
	if turnover <= 0 {
		return Charges{}
	}

	c := Charges{
		Brokerage:   brokeragePerOrder,
		Transaction: turnover * transactionRate,
		SEBI:        turnover * sebiRate,
		IPFT:        turnover * ipftRate,
	}
	if side == SideSell {
		// STT on options is charged on the sell side only, on premium.
		c.STT = turnover * sttSellRate
	} else {
		// Stamp duty mirrors it — buy side only.
		c.StampDuty = turnover * stampDutyBuyRate
	}
	// GST applies to the broker/exchange/regulator fees, never to STT or
	// stamp duty (those are taxes in their own right, not services).
	c.GST = (c.Brokerage + c.Transaction + c.SEBI + c.IPFT) * gstRate
	c.Total = c.Brokerage + c.STT + c.Transaction + c.SEBI + c.IPFT + c.StampDuty + c.GST
	return c
}

// chargesFor is the single gate deciding whether an order incurs modelled
// charges at all: options do, equity does not (its rate card isn't
// implemented — see OptionCharges). Every open/close path routes through
// this rather than testing OptionType inline, so equity can be added in one
// place later.
func chargesFor(optionType, side string, premium float64, qty int) Charges {
	if optionType == "" {
		return Charges{}
	}
	return OptionCharges(side, premium, qty)
}

// EstimateIncomeTax is a DISPLAY-ONLY estimate of income tax owed on a net
// F&O profit. F&O gains are taxed as non-speculative business income at the
// individual's slab rate — which this package cannot know — so this assumes
// the top 30% slab and is deliberately never applied to any balance. It
// exists so the statistics screen can show "and roughly this much again
// goes to income tax at year end" rather than implying net P&L is take-home.
// Returns 0 for a loss (losses carry forward; no tax owed).
func EstimateIncomeTax(netProfit float64) float64 {
	if netProfit <= 0 {
		return 0
	}
	return netProfit * incomeTaxEstimateSlab
}
