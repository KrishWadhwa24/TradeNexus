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

	var volSum int64
	for _, b := range bars {
		volSum += b.Volume
	}
	avgVol := float64(volSum) / float64(len(bars))
	currentVol := float64(bars[len(bars)-1].Volume)

	in := EntryInputs{
		Direction:              direction,
		Spot:                   niftyInputs.Spot,
		VWAP:                   niftyInputs.VWAP,
		ATR:                    niftyInputs.ATR,
		OptionLTP:              selected.LTP,
		OptionOR:               or,
		OptionVolume:           currentVol,
		OptionAvgVolume:        avgVol,
		MinVolumeMultiplier:    cfg.MinVolumeMultiplier,
		MaxDistanceFromVWAPATR: cfg.MaxDistanceFromVWAPATR,
	}
	return EvaluateEntry(in), nil
}
