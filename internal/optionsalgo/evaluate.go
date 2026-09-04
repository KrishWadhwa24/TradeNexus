package optionsalgo

import (
	"context"
	"errors"
	"time"

	"tradenexus/internal/market"
)

// evalHistoryBars is how many recent 1-minute bars to pull for direction
// evaluation — generous enough for EMA50 on 15-minute bars (50 * 15 = 750
// minutes minimum to seed) to have converged over several days, not just
// barely seeded.
const evalHistoryBars = 5000

// EvaluateDirection resolves live indicator inputs from stored candles (spot
// NIFTY + the tracked NIFTY future) and runs DetermineDirection — the
// orchestration layer around direction.go's pure functions. Read-only: does
// not place or affect any trade. Phase 4's execution bridge will call this
// same method; for now it's exercised via the admin verification endpoint.
func (s *Service) EvaluateDirection(ctx context.Context) (DirectionResult, DirectionInputs, error) {
	cfg, err := s.repo.GetConfig(ctx)
	if err != nil {
		return DirectionResult{}, DirectionInputs{}, err
	}
	underlyings, err := s.repo.TrackedUnderlyings(ctx)
	if err != nil {
		return DirectionResult{}, DirectionInputs{}, err
	}
	var spotID int64
	for _, u := range underlyings {
		if u.UnderlyingSymbol == "NIFTY" {
			spotID = u.ID
		}
	}
	if spotID == 0 {
		return DirectionResult{}, DirectionInputs{}, errors.New("NIFTY spot instrument not tracked")
	}

	futures, err := s.repo.TrackedFutures(ctx)
	if err != nil {
		return DirectionResult{}, DirectionInputs{}, err
	}
	if len(futures) == 0 {
		return DirectionResult{}, DirectionInputs{}, errors.New("NIFTY future not tracked (run derivatives sync)")
	}

	spotBars, err := s.repo.GetMinuteCandles(ctx, spotID, evalHistoryBars)
	if err != nil {
		return DirectionResult{}, DirectionInputs{}, err
	}
	if len(spotBars) == 0 {
		return DirectionResult{}, DirectionInputs{}, errors.New("no spot candles stored yet")
	}
	futBars, err := s.repo.GetMinuteCandles(ctx, futures[0].ID, evalHistoryBars)
	if err != nil {
		return DirectionResult{}, DirectionInputs{}, err
	}
	if len(futBars) == 0 {
		return DirectionResult{}, DirectionInputs{}, errors.New("no future candles stored yet")
	}

	fifteenMin := Aggregate15Min(spotBars)
	emaFastSeries := EMA(fifteenMin, cfg.EMAFastPeriod)
	emaSlowSeries := EMA(fifteenMin, cfg.EMASlowPeriod)
	atrSeries := ATR(fifteenMin, cfg.ATRPeriod)
	atrAvgSeries := ATRAverage(atrSeries, cfg.ATRAvgSpan)
	vwapSeries := SessionVWAP(futBars)

	now := time.Now().In(market.IST)
	or := BuildOpeningRange(spotBars, now, cfg)

	in := DirectionInputs{
		Spot:            spotBars[len(spotBars)-1].Close,
		OR:              or,
		VWAP:            vwapSeries[len(vwapSeries)-1],
		EMAFast:         emaFastSeries[len(emaFastSeries)-1],
		EMASlow:         emaSlowSeries[len(emaSlowSeries)-1],
		ATR:             atrSeries[len(atrSeries)-1],
		ATRAvg:          atrAvgSeries[len(atrAvgSeries)-1],
		MinRangePercent: cfg.ORMinRangePercent,
	}
	return DetermineDirection(in), in, nil
}
