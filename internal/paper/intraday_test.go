package paper

import "testing"

func TestSettleAmounts(t *testing.T) {
	cases := []struct {
		name           string
		side           string
		productType    string
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			trade := Trade{Side: c.side, ProductType: c.productType, EntryPrice: c.entry, Quantity: c.qty}
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
	if marginFraction(ProductDelivery) != 1.0 {
		t.Error("delivery must require full (1.0) margin")
	}
	if marginFraction(ProductIntraday) != intradayMarginFraction {
		t.Errorf("intraday margin fraction = %v, want %v", marginFraction(ProductIntraday), intradayMarginFraction)
	}
}
