package optionsalgo

import (
	"context"
	"time"

	"tradenexus/internal/angel"
	"tradenexus/internal/market"
)

// EvaluateEntryForSelected fetches the selected contract's own today's
// 1-minute history — on demand, only for the one contract Phase 2 selected,
// not the whole chain — to build its opening range and current/average
// volume, then runs the pure entry gate (entry.go). niftyInputs must be
// freshly resolved (e.g. from the same EvaluateDirection call that produced
// `direction`), not cached from an earlier tick — see EntryInputs' doc
// comment.
func (s *Service) EvaluateEntryForSelected(ctx context.Context, direction Direction, niftyInputs DirectionInputs, selected OptionQuote) (EntryDecision, error) {
	cfg, err := s.repo.GetConfig(ctx)
	if err != nil {
		return EntryDecision{}, err
	}

	now := time.Now().In(market.IST)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, market.IST)

	bars, err := s.angel.GetIntradayCandles(ctx, "NFO", selected.Token, angel.IntervalOneMinute, dayStart, now)
	if err != nil {
		return EntryDecision{}, err
	}
	if len(bars) == 0 {
		return EntryDecision{false, "no intraday history yet for the selected contract"}, nil
	}

	or := BuildOpeningRange(bars, now, cfg)

	// The final bar is the minute currently in progress — Angel returns it
	// partially filled, so it may hold only a few seconds of volume. It has
	// to be dropped from BOTH sides of the comparison: using it as
	// "current" while averaging completed minutes compares seconds against
	// full minutes, which understates current volume for most of the
	// session and wrongly rejects genuine breakouts (and, with very few
	// bars just after the opening range forms, makes current == average so
	// the gate passes trivially — wrong in both directions).
	//
	// After dropping it, "current" is the last COMPLETED minute and the
	// average covers the completed minutes before it — like for like.
	complete := bars[:len(bars)-1]
	if len(complete) < 2 {
		return EntryDecision{false, "not enough completed 1-minute bars yet to judge the contract's volume"}, nil
	}
	currentVol := float64(complete[len(complete)-1].Volume)

	var volSum int64
	for _, b := range complete[:len(complete)-1] {
		volSum += b.Volume
	}
	avgVol := float64(volSum) / float64(len(complete)-1)

	in := EntryInputs{
		Direction: direction,
		Spot:      niftyInputs.Spot,
		VWAP:      niftyInputs.VWAP,
		ATR:       niftyInputs.ATR,
		// EffectivePrice, not raw LTP — see OptionQuote.EffectivePrice.
		OptionLTP:              selected.EffectivePrice(),
		OptionOR:               or,
		OptionVolume:           currentVol,
		OptionAvgVolume:        avgVol,
		MinVolumeMultiplier:    cfg.MinVolumeMultiplier,
		MaxDistanceFromVWAPATR: cfg.MaxDistanceFromVWAPATR,
	}
	return EvaluateEntry(in), nil
}
