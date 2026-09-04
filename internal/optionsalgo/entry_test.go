package optionsalgo

import "testing"

func baseEntryInputs() EntryInputs {
	return EntryInputs{
		Direction:       Bullish,
		Spot:            23950,
		VWAP:            23900,
		ATR:             40, // distance = 50/40 = 1.25, within 1.5 limit
		OptionLTP:       125,
		OptionOR:        OpeningRange{High: 120, Low: 100, RangePercent: 1, Valid: true},
		OptionVolume:    1500,
		OptionAvgVolume: 1000, // 1500 >= 1000*1.2

		MinVolumeMultiplier:    1.2,
		MaxDistanceFromVWAPATR: 1.5,
	}
}

func TestEvaluateEntry_Passes(t *testing.T) {
	got := EvaluateEntry(baseEntryInputs())
	if !got.ShouldEnter {
		t.Errorf("expected entry, got reason: %s", got.Reason)
	}
}

func TestEvaluateEntry_NoDirection(t *testing.T) {
	in := baseEntryInputs()
	in.Direction = NoneDir
	got := EvaluateEntry(in)
	if got.ShouldEnter {
		t.Error("expected no entry when direction is NONE")
	}
}

func TestEvaluateEntry_ORNotEstablished(t *testing.T) {
	in := baseEntryInputs()
	in.OptionOR = OpeningRange{}
	got := EvaluateEntry(in)
	if got.ShouldEnter {
		t.Error("expected no entry when option OR isn't established")
	}
}

func TestEvaluateEntry_PremiumHasNotBrokenOut(t *testing.T) {
	in := baseEntryInputs()
	in.OptionLTP = 115 // below OR.High of 120
	got := EvaluateEntry(in)
	if got.ShouldEnter {
		t.Error("expected no entry when premium hasn't broken its OR high")
	}
}

func TestEvaluateEntry_PremiumExactlyAtHigh_NotABreak(t *testing.T) {
	in := baseEntryInputs()
	in.OptionLTP = in.OptionOR.High // equal, not strictly above
	got := EvaluateEntry(in)
	if got.ShouldEnter {
		t.Error("expected no entry when premium == OR high (needs to break above, not just reach)")
	}
}

func TestEvaluateEntry_VolumeNotElevated(t *testing.T) {
	in := baseEntryInputs()
	in.OptionVolume = 1100 // needs >= 1000*1.2 = 1200
	got := EvaluateEntry(in)
	if got.ShouldEnter {
		t.Error("expected no entry when volume isn't elevated vs its own average")
	}
}

func TestEvaluateEntry_TooFarFromVWAP(t *testing.T) {
	in := baseEntryInputs()
	in.Spot = 24100 // distance = 200/40 = 5.0, way over 1.5
	got := EvaluateEntry(in)
	if got.ShouldEnter {
		t.Error("expected no entry when NIFTY is too far from VWAP relative to ATR")
	}
}

func TestEvaluateEntry_Bearish_SamePremiumBreakoutRule(t *testing.T) {
	// Script uses OPTION_LTP > OPTION_OR_HIGH for PE too (a put's premium
	// still needs to break upward to confirm strength) — not OR_LOW.
	in := baseEntryInputs()
	in.Direction = Bearish
	in.Spot = 23850
	in.VWAP = 23900 // distance = 50/40 = 1.25, still within limit
	got := EvaluateEntry(in)
	if !got.ShouldEnter {
		t.Errorf("expected entry for bearish case with same OR-high breakout rule, got: %s", got.Reason)
	}
}
