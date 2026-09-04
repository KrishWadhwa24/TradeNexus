package optionsalgo

// PositionSize computes how many units (already a multiple of lotSize) to
// buy for one algo entry: risk% of current algo capital / the stop-loss
// distance in rupees = raw quantity, floored to whole lots. Zero is a valid,
// expected outcome — the script's own rule is "don't trade" when sizing
// rounds to zero (e.g. capital too small for this premium/lot size), not
// "trade the smallest amount anyway."
func PositionSize(algoCapital, riskPerTradePercent, entryPrice, stopLossPercent float64, lotSize int) int {
	if entryPrice <= 0 || lotSize <= 0 || stopLossPercent <= 0 {
		return 0
	}
	maxRisk := algoCapital * riskPerTradePercent / 100
	stopDistance := entryPrice * stopLossPercent / 100
	if stopDistance <= 0 || maxRisk <= 0 {
		return 0
	}
	qtyByRisk := maxRisk / stopDistance
	lots := int(qtyByRisk) / lotSize
	return lots * lotSize
}
