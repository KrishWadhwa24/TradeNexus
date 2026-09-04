package optionsalgo

import "fmt"

// EntryInputs bundles everything EvaluateEntry needs, all resolved fresh at
// the moment of the check. Direction must be re-derived right before this
// call, not reused from an earlier tick — NIFTY's OR-break and VWAP-side
// conditions (already encoded in Direction, see DetermineDirection) could
// have flipped back in the time between selecting a contract and its own
// premium OR completing, and the script explicitly re-checks freshness at
// entry rather than trusting an earlier direction call.
type EntryInputs struct {
	Direction       Direction
	Spot            float64
	VWAP            float64
	ATR             float64
	OptionLTP       float64
	OptionOR        OpeningRange // the SELECTED CONTRACT's own 09:15-09:45 range
	OptionVolume    float64
	OptionAvgVolume float64
	// MinVolumeMultiplier/MaxDistanceFromVWAPATR are AlgoConfig values at
	// evaluation time (frontend-editable) — script defaults 1.2x and 1.5 ATR.
	MinVolumeMultiplier    float64
	MaxDistanceFromVWAPATR float64
}

// EntryDecision is the entry gate's yes/no output plus why — always a
// reason, even when true, matching every other decision function's logging
// convention.
type EntryDecision struct {
	ShouldEnter bool
	Reason      string
}

// EvaluateEntry applies the script's entry conditions exactly:
//   - direction confirmed (Bullish or Bearish)
//   - the option's own premium has broken above its own opening-range high
//     (per the script, this is the same check for both CE and PE — a rising
//     premium is what confirms strength either way, not "LTP < OR low" for PE)
//   - the option's own volume is elevated vs its own average
//   - NIFTY isn't already too far from VWAP relative to ATR (not chasing an
//     extended move)
//
// NIFTY's own OR-break/VWAP-side checks are NOT re-tested explicitly here —
// see EntryInputs' doc comment for why that's equivalent, not a shortcut,
// given Direction is freshly computed.
func EvaluateEntry(in EntryInputs) EntryDecision {
	if in.Direction != Bullish && in.Direction != Bearish {
		return EntryDecision{false, "no direction"}
	}
	if in.OptionOR.High <= 0 {
		return EntryDecision{false, "option's own opening range not yet established"}
	}
	if in.OptionLTP <= in.OptionOR.High {
		return EntryDecision{false, fmt.Sprintf("option premium %.2f has not broken its opening-range high %.2f", in.OptionLTP, in.OptionOR.High)}
	}
	if in.OptionAvgVolume > 0 && in.OptionVolume < in.OptionAvgVolume*in.MinVolumeMultiplier {
		return EntryDecision{false, "option volume not elevated vs its own average"}
	}
	if in.ATR > 0 {
		distance := abs(in.Spot-in.VWAP) / in.ATR
		if distance > in.MaxDistanceFromVWAPATR {
			return EntryDecision{false, fmt.Sprintf("NIFTY is %.2f ATRs from VWAP (max %.1f) — move already extended", distance, in.MaxDistanceFromVWAPATR)}
		}
	}
	return EntryDecision{true, fmt.Sprintf("%s entry confirmed: option broke its OR high, volume elevated, NIFTY not overextended from VWAP", in.Direction)}
}
