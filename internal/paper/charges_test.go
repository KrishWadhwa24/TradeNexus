package paper

import (
	"math"
	"testing"
)

func closeTo(t *testing.T, label string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.4f, want %.4f (tolerance %.4f)", label, got, want, tol)
	}
}

// TestOptionCharges_BuySide hand-computes every line for one real-shaped
// order: 65 units (1 NIFTY lot) at Rs.100 premium => Rs.6,500 turnover.
func TestOptionCharges_BuySide(t *testing.T) {
	c := OptionCharges(SideBuy, 100, 65)

	closeTo(t, "brokerage", c.Brokerage, 20.0, 0.0001)
	closeTo(t, "stt", c.STT, 0, 0.0001) // buy side pays no STT on options
	closeTo(t, "transaction", c.Transaction, 6500*0.0003553, 0.0001)
	closeTo(t, "sebi", c.SEBI, 6500*0.000001, 0.0001)
	closeTo(t, "stamp duty", c.StampDuty, 6500*0.00003, 0.0001)

	// GST is 18% of (brokerage + transaction + SEBI + IPFT) — never of
	// stamp duty or STT.
	wantGST := (20.0 + 6500*0.0003553 + 6500*0.000001 + 6500*0.000000001) * 0.18
	closeTo(t, "gst", c.GST, wantGST, 0.0001)

	wantTotal := 20.0 + 6500*0.0003553 + 6500*0.000001 + 6500*0.000000001 + 6500*0.00003 + wantGST
	closeTo(t, "total", c.Total, wantTotal, 0.0001)

	// Sanity: a 1-lot buy at Rs.100 should cost roughly Rs.26-27 all in.
	if c.Total < 24 || c.Total > 29 {
		t.Errorf("total %.2f outside the expected ~Rs.26 range for a 1-lot Rs.100 buy", c.Total)
	}
}

// TestOptionCharges_SellSideChargesSTTNotStampDuty is the asymmetry that
// matters most: STT (0.15%) is the single largest line on an options exit
// and applies ONLY on the sell, while stamp duty applies ONLY on the buy.
func TestOptionCharges_SellSideChargesSTTNotStampDuty(t *testing.T) {
	c := OptionCharges(SideSell, 120, 65) // Rs.7,800 turnover

	closeTo(t, "stt", c.STT, 7800*0.0015, 0.0001)
	if c.StampDuty != 0 {
		t.Errorf("stamp duty = %.4f, want 0 — it is a buy-side-only charge", c.StampDuty)
	}
	if c.STT < 11 {
		t.Errorf("STT %.2f looks too small — 0.15%% of Rs.7,800 should be ~Rs.11.70", c.STT)
	}
}

// TestOptionCharges_RoundTripIsMaterial guards the whole point of this
// package: a full round trip on a single lot must cost enough to visibly
// change P&L. If someone "optimises" a rate to near-zero this fails loudly.
func TestOptionCharges_RoundTripIsMaterial(t *testing.T) {
	buy := OptionCharges(SideBuy, 100, 65)
	sell := OptionCharges(SideSell, 120, 65)
	roundTrip := buy.Total + sell.Total

	if roundTrip < 50 || roundTrip > 80 {
		t.Errorf("round-trip cost %.2f outside the expected ~Rs.65 range for 1 lot bought at 100 and sold at 120", roundTrip)
	}
	// Gross profit on that trade is (120-100)*65 = Rs.1,300. Charges must
	// eat a real, visible slice of it — around 5%.
	gross := (120.0 - 100.0) * 65
	if pct := roundTrip / gross * 100; pct < 3 || pct > 8 {
		t.Errorf("charges are %.1f%% of gross profit, expected ~5%% — the cost model looks wrong", pct)
	}
}

// TestOptionCharges_ZeroTurnover covers the degenerate inputs the live
// paths can hand us (a zero price from a failed quote, a zero-qty lot).
func TestOptionCharges_ZeroTurnover(t *testing.T) {
	for _, tc := range []struct {
		name    string
		premium float64
		qty     int
	}{
		{"zero qty", 100, 0},
		{"negative premium", -5, 65},
	} {
		if got := OptionCharges(SideBuy, tc.premium, tc.qty); got.Total != 0 {
			t.Errorf("%s: total = %.4f, want 0 (not a real order — must not bill a flat brokerage)", tc.name, got.Total)
		}
	}
}

// TestOptionCharges_WorthlessExitStillPaysBrokerage: squaring off an option
// that has gone to zero is still an executed order. Every turnover-based
// line is zero, but brokerage + GST are still owed — and these are exactly
// the trades a stop-loss strategy produces most often, so getting this
// wrong makes losing trades look systematically cheaper than they are.
func TestOptionCharges_WorthlessExitStillPaysBrokerage(t *testing.T) {
	c := OptionCharges(SideSell, 0, 65)
	closeTo(t, "brokerage", c.Brokerage, 20.0, 0.0001)
	closeTo(t, "gst", c.GST, 20.0*0.18, 0.0001)
	closeTo(t, "stt", c.STT, 0, 0.0001) // zero turnover => zero STT
	closeTo(t, "total", c.Total, 20.0*1.18, 0.0001)
}

// TestChargesFor_EquityIsNotCharged pins the deliberate scope limit: the
// equity cash-segment rate card isn't implemented, so equity trades must
// come back with zero rather than being silently billed the options rates.
func TestChargesFor_EquityIsNotCharged(t *testing.T) {
	if got := chargesFor("", SideBuy, 1500, 10); got.Total != 0 {
		t.Errorf("equity charges = %.4f, want 0 — the equity rate card is not modelled", got.Total)
	}
	if got := chargesFor("CE", SideBuy, 100, 65); got.Total <= 0 {
		t.Error("expected an option order to be charged")
	}
}

func TestEstimateIncomeTax(t *testing.T) {
	closeTo(t, "profit", EstimateIncomeTax(10000), 3000, 0.0001)
	if got := EstimateIncomeTax(-5000); got != 0 {
		t.Errorf("EstimateIncomeTax(loss) = %.2f, want 0 — a loss owes no tax", got)
	}
	if got := EstimateIncomeTax(0); got != 0 {
		t.Errorf("EstimateIncomeTax(0) = %.2f, want 0", got)
	}
}
