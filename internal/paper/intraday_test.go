package paper

import (
	"testing"

	"tradenexus/internal/instruments"
)

func TestSettleAmounts(t *testing.T) {
	cases := []struct {
		name           string
		side           string
		productType    string
		optionType     string
		entry, exit    float64
		qty            int
		wantPnL        float64
		wantSettlement float64
	}{
		{
			// Delivery long: full notional was debited at open (100%), so
			// closing must return exactly exitPrice*qty — this must match
			// today's pre-existing behavior exactly, unchanged.
			name: "delivery long, profit", side: SideBuy, productType: ProductDelivery,
			entry: 100, exit: 120, qty: 10,
			wantPnL: 200, wantSettlement: 1200, // = exit*qty
		},
		{
			name: "delivery long, loss", side: SideBuy, productType: ProductDelivery,
			entry: 100, exit: 90, qty: 10,
			wantPnL: -100, wantSettlement: 900, // = exit*qty
		},
		{
			// Intraday long: only 20% (200) was debited at open. Profit
			// case: price rises 100->120, pnl=200, settlement = 200(margin)+200(pnl) = 400.
			name: "intraday long, profit", side: SideBuy, productType: ProductIntraday,
			entry: 100, exit: 120, qty: 10,
			wantPnL: 200, wantSettlement: 400,
		},
		{
			// Intraday short: profits when price falls. 20% margin (200)
			// reserved. Price falls 100->90, pnl=(100-90)*10=100, settlement=200+100=300.
			name: "intraday short, profit (price fell)", side: SideSell, productType: ProductIntraday,
			entry: 100, exit: 90, qty: 10,
			wantPnL: 100, wantSettlement: 300,
		},
		{
			// Intraday short, loss: price rises against the short.
			name: "intraday short, loss (price rose)", side: SideSell, productType: ProductIntraday,
			entry: 100, exit: 110, qty: 10,
			wantPnL: -100, wantSettlement: 100, // 200 margin - 100 loss
		},
		{
			// Regression case: an option bought as "INTRADAY" must still
			// settle at full premium (fraction 1.0), NOT the equity 20%
			// intraday margin — a long option has no leverage concept.
			// Without OptionType set, this would wrongly compute
			// settlement=400 (the equity-intraday case above); with it,
			// it must match the delivery-long numbers exactly (fraction 1.0).
			name: "option (CE) intraday, profit — must margin at 100%, not 20%",
			side: SideBuy, productType: ProductIntraday, optionType: "CE",
			entry: 100, exit: 120, qty: 10,
			wantPnL: 200, wantSettlement: 1200, // = exit*qty, same as delivery
		},
		{
			name: "option (PE) intraday, loss — must margin at 100%, not 20%",
			side: SideBuy, productType: ProductIntraday, optionType: "PE",
			entry: 100, exit: 90, qty: 10,
			wantPnL: -100, wantSettlement: 900, // = exit*qty
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			trade := Trade{Side: c.side, ProductType: c.productType, OptionType: c.optionType, EntryPrice: c.entry, Quantity: c.qty}
			pnl, settlement := settleAmounts(trade, c.exit)
			if pnl != c.wantPnL {
				t.Errorf("pnl = %v, want %v", pnl, c.wantPnL)
			}
			if settlement != c.wantSettlement {
				t.Errorf("settlement = %v, want %v", settlement, c.wantSettlement)
			}
		})
	}
}

func TestWeightedAvgEntry(t *testing.T) {
	cases := []struct {
		name      string
		qty1      int
		price1    float64
		qty2      int
		price2    float64
		wantQty   int
		wantPrice float64
	}{
		{"equal size, different price", 10, 100, 10, 120, 20, 110},
		{"unequal size weights toward larger fill", 10, 100, 30, 140, 40, 130},
		{"second fill at same price", 5, 200, 5, 200, 10, 200},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			qty, price := weightedAvgEntry(c.qty1, c.price1, c.qty2, c.price2)
			if qty != c.wantQty {
				t.Errorf("qty = %v, want %v", qty, c.wantQty)
			}
			if price != c.wantPrice {
				t.Errorf("price = %v, want %v", price, c.wantPrice)
			}
		})
	}
}

func TestMarginFraction(t *testing.T) {
	if marginFraction(ProductDelivery, false) != 1.0 {
		t.Error("equity delivery must require full (1.0) margin")
	}
	if marginFraction(ProductIntraday, false) != intradayMarginFraction {
		t.Errorf("equity intraday margin fraction = %v, want %v", marginFraction(ProductIntraday, false), intradayMarginFraction)
	}
	// An option always costs its full premium upfront — no leverage/margin
	// concept for a long option — regardless of product type.
	if marginFraction(ProductIntraday, true) != 1.0 {
		t.Error("option intraday must still require full (1.0) margin — no leverage on a long option")
	}
	if marginFraction(ProductDelivery, true) != 1.0 {
		t.Error("option delivery must require full (1.0) margin")
	}
}

func TestValidateOptionLotSize(t *testing.T) {
	equity := instruments.Instrument{OptionType: "", LotSize: 1}
	niftyOption := instruments.Instrument{OptionType: "CE", LotSize: 75}

	cases := []struct {
		name    string
		inst    instruments.Instrument
		qty     int
		wantErr bool
	}{
		{"equity, any quantity is valid", equity, 7, false},
		{"equity, lot size doesn't even apply", equity, 1, false},
		{"option, exact one lot", niftyOption, 75, false},
		{"option, exact multiple of lot size", niftyOption, 225, false},
		{"option, not a multiple of lot size", niftyOption, 50, true},
		{"option, less than one lot", niftyOption, 10, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateOptionLotSize(c.inst, c.qty)
			if (err != nil) != c.wantErr {
				t.Errorf("validateOptionLotSize(qty=%d) error = %v, wantErr %v", c.qty, err, c.wantErr)
			}
		})
	}
}
