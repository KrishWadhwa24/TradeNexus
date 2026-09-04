package optionsalgo

import (
	"context"
	"fmt"
	"time"

	"tradenexus/internal/market"
	"tradenexus/internal/paper"
)

// EntryOutcome is what one evaluate-and-maybe-enter pass produced, for
// logging and live-verification — always populated, whether or not a trade
// was actually placed.
type EntryOutcome struct {
	Direction  Direction
	Selected   OptionQuote
	EntryOK    bool
	EntryError string
	Sized      int
	Traded     bool
	TradeID    int64
	Reason     string
}

// EvaluateAndMaybeEnter runs the full pipeline — direction, chain, select,
// entry gate, risk governance (daily/weekly loss circuit breakers, max-1-
// open-position, max-trades/day), position sizing — and places a real trade
// only if every step clears. algoUserID is the account whose AlgoCashBalance
// funds the trade and whose paper_trades row gets source=SourceOptionsAlgo.
// Safe to call repeatedly (e.g. once a minute); each call is independent.
//
// Every call logs exactly one algo_decisions row via the deferred call
// below, whatever the outcome — per the script's explicit requirement to
// record every no-trade evaluation, not just executed trades.
func (s *Service) EvaluateAndMaybeEnter(ctx context.Context, algoUserID string) (EntryOutcome, error) {
	d := Decision{EvaluatedAt: time.Now().In(market.IST)}
	defer func() {
		if err := s.repo.LogDecision(ctx, d); err != nil {
			s.log.Error().Err(err).Msg("optionsalgo: failed to log decision")
		}
	}()

	cfg, err := s.repo.GetConfig(ctx)
	if err != nil {
		d.Action, d.Detail = "ERROR", err.Error()
		return EntryOutcome{}, err
	}

	direction, inputs, err := s.EvaluateDirection(ctx)
	if err != nil {
		d.Action, d.Detail = "ERROR", err.Error()
		return EntryOutcome{}, err
	}
	d.NiftySpot, d.ORHigh, d.ORLow = inputs.Spot, inputs.OR.High, inputs.OR.Low
	d.VWAP, d.EMAFast, d.EMASlow = inputs.VWAP, inputs.EMAFast, inputs.EMASlow
	d.ATR, d.ATRAvg = inputs.ATR, inputs.ATRAvg
	d.Direction, d.DirectionReason = string(direction.Direction), direction.Reason

	out := EntryOutcome{Direction: direction.Direction}
	if direction.Direction == NoneDir {
		d.Action = "NO_DIRECTION"
		out.Reason = direction.Reason
		return out, nil
	}

	acct, err := s.paper.GetAccount(ctx, algoUserID)
	if err != nil {
		d.Action, d.Detail = "ERROR", err.Error()
		return out, err
	}
	risk, err := s.algoRiskSnapshot(ctx, algoUserID, d.EvaluatedAt)
	if err != nil {
		d.Action, d.Detail = "ERROR", err.Error()
		return out, err
	}

	// Circuit breakers first — the most severe "stop everything" cases,
	// checked before anything else regardless of how good this tick's
	// signal looks.
	dailyLimit := -(acct.AlgoCashBalance * cfg.MaxDailyLossPercent / 100)
	if risk.dailyPnL <= dailyLimit {
		d.Action = "DAILY_LOSS_LIMIT"
		out.Reason = fmt.Sprintf("today's algo P&L (%.2f) has hit the %.1f%% daily loss limit", risk.dailyPnL, cfg.MaxDailyLossPercent)
		d.Detail = out.Reason
		return out, nil
	}
	weeklyLimit := -(acct.AlgoCashBalance * cfg.MaxWeeklyLossPercent / 100)
	if risk.weeklyPnL <= weeklyLimit {
		d.Action = "WEEKLY_LOSS_LIMIT"
		out.Reason = fmt.Sprintf("this week's algo P&L (%.2f) has hit the %.1f%% weekly loss limit", risk.weeklyPnL, cfg.MaxWeeklyLossPercent)
		d.Detail = out.Reason
		return out, nil
	}
	// Safety net kept in now (not deferred): never more than one open algo
	// position at a time, and never more than the configured trades/day,
	// regardless of signal quality.
	if risk.open > 0 {
		d.Action = "MAX_POSITIONS"
		out.Reason = "an algo position is already open — max 1 at a time"
		return out, nil
	}
	if risk.tradesToday >= cfg.MaxTradesPerDay {
		d.Action = "MAX_TRADES_TODAY"
		out.Reason = fmt.Sprintf("already placed %d trade(s) today (max %d)", risk.tradesToday, cfg.MaxTradesPerDay)
		return out, nil
	}

	chain, err := s.BuildOptionChain(ctx, inputs.Spot)
	if err != nil {
		d.Action, d.Detail = "ERROR", "chain: "+err.Error()
		return EntryOutcome{}, fmt.Errorf("chain: %w", err)
	}
	selected, selReason, ok := s.SelectContract(ctx, direction.Direction, chain)
	out.Selected = selected
	d.SelectedSymbol, d.SelectedStrike = selected.TradingSymbol, selected.StrikePrice
	d.SelectedDelta, d.SelectedIV, d.SelectedTheta = selected.Delta, selected.IV, selected.Theta
	d.SelectionReason = selReason
	if !ok {
		d.Action = "NO_CONTRACT"
		out.Reason = selReason
		return out, nil
	}

	entry, err := s.EvaluateEntryForSelected(ctx, direction.Direction, inputs, selected)
	if err != nil {
		d.Action, d.Detail = "ERROR", "entry: "+err.Error()
		return EntryOutcome{}, fmt.Errorf("entry: %w", err)
	}
	out.EntryOK = entry.ShouldEnter
	out.Reason = entry.Reason
	d.EntryOK, d.EntryReason = entry.ShouldEnter, entry.Reason
	if !entry.ShouldEnter {
		d.Action = "ENTRY_REJECTED"
		return out, nil
	}

	// EffectivePrice, not raw LTP — see OptionQuote.EffectivePrice.
	qty := PositionSize(acct.AlgoCashBalance, cfg.RiskPerTradePercent, selected.EffectivePrice(), cfg.InitialStopLossPercent, selected.LotSize)
	out.Sized = qty
	if qty <= 0 {
		d.Action = "SIZED_ZERO"
		out.Reason = "position sizing rounded to 0 lots — algo capital too small for this premium at the configured risk"
		return out, nil
	}

	// DELIVERY, not INTRADAY — the existing 3:20pm auto-square-off cron only
	// ever touches INTRADAY rows, so this is what lets the position ride
	// past today without any new square-off-exemption code. Margin is still
	// 100% of premium either way (Step 1), unaffected by this choice.
	trade, err := s.paper.OpenPosition(ctx, algoUserID, selected.InstrumentID, qty, paper.SideBuy, paper.ProductDelivery, nil, paper.SourceOptionsAlgo)
	if err != nil {
		d.Action = "ORDER_REJECTED"
		out.Reason = "order rejected: " + err.Error()
		return out, nil
	}
	out.Traded = true
	out.TradeID = trade.ID
	d.Action = "EXECUTED"
	d.TradeID = &trade.ID

	if err := s.repo.UpsertPositionState(ctx, PositionState{
		TradeID:      trade.ID,
		HighestPrice: trade.EntryPrice,
		CurrentStop:  InitialStop(trade.EntryPrice, cfg.InitialStopLossPercent),
	}); err != nil {
		s.log.Error().Err(err).Int64("trade_id", trade.ID).Msg("optionsalgo: failed to init position state")
	}
	return out, nil
}

// algoRisk bundles the risk-governance snapshot for one evaluation tick.
type algoRisk struct {
	open        int
	tradesToday int
	dailyPnL    float64
	weeklyPnL   float64
}

// algoRiskSnapshot computes everything EvaluateAndMaybeEnter's risk checks
// need from a single paper.Trades call — open-position count, trades placed
// today, and today's/this week's realized algo P&L.
func (s *Service) algoRiskSnapshot(ctx context.Context, algoUserID string, now time.Time) (algoRisk, error) {
	trades, err := s.paper.Trades(ctx, algoUserID)
	if err != nil {
		return algoRisk{}, err
	}
	var r algoRisk
	for _, t := range trades {
		if t.Source != paper.SourceOptionsAlgo {
			continue
		}
		if t.Status == "OPEN" {
			r.open++
		}
		if t.EntryTime != nil && market.SameISTDate(*t.EntryTime, now) {
			r.tradesToday++
		}
	}
	r.dailyPnL = dailyAlgoPnL(trades, now)
	r.weeklyPnL = weeklyAlgoPnL(trades, now)
	return r, nil
}

// ManagementOutcome describes what happened to one open algo position on one
// management tick.
type ManagementOutcome struct {
	TradeID    int64
	Symbol     string
	Exited     bool
	ExitReason string
}

// ManageOpenPositions runs one management tick over every OPEN algo
// position: expiry-day force-exit (checked first — an option can't be
// allowed to actually expire in the sim, regardless of "hold if you believe
// in tomorrow"), then the stop/breakeven/trailing rules, then the
// underlying-exit-confirmation (NIFTY's own direction reversing) as an
// additional, non-exclusive exit signal. Every exit logs an algo_decisions
// row (Action="EXIT") with the P&L and MFE/MAE for that position's life.
func (s *Service) ManageOpenPositions(ctx context.Context, algoUserID string) ([]ManagementOutcome, error) {
	cfg, err := s.repo.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	trades, err := s.paper.Trades(ctx, algoUserID)
	if err != nil {
		return nil, err
	}

	var direction DirectionResult
	var directionInputs DirectionInputs
	var haveDirection bool

	var outcomes []ManagementOutcome
	now := time.Now().In(market.IST)
	for _, t := range trades {
		if t.Status != "OPEN" || t.Source != paper.SourceOptionsAlgo {
			continue
		}
		outcome := ManagementOutcome{TradeID: t.ID, Symbol: t.Symbol}

		expiry, err := s.repo.GetInstrumentExpiry(ctx, t.InstrumentID)
		if err != nil {
			s.log.Error().Err(err).Int64("trade_id", t.ID).Msg("optionsalgo: expiry lookup failed")
		} else if expiry != nil && !now.In(market.IST).Before(time.Date(expiry.Year(), expiry.Month(), expiry.Day(), 0, 0, 0, 0, market.IST)) {
			outcome.Exited, outcome.ExitReason = s.exitPosition(ctx, t, "expiry day — contract cannot be held through expiry")
			outcomes = append(outcomes, outcome)
			continue
		}

		currentPrice, err := s.currentOptionPrice(ctx, t.InstrumentID)
		if err != nil {
			s.log.Error().Err(err).Int64("trade_id", t.ID).Msg("optionsalgo: live price fetch failed, skipping this tick")
			outcomes = append(outcomes, outcome)
			continue
		}

		state, found, err := s.repo.GetPositionState(ctx, t.ID)
		if err != nil {
			s.log.Error().Err(err).Int64("trade_id", t.ID).Msg("optionsalgo: position state load failed")
			outcomes = append(outcomes, outcome)
			continue
		}
		if !found {
			state = PositionState{TradeID: t.ID, HighestPrice: t.EntryPrice, CurrentStop: InitialStop(t.EntryPrice, cfg.InitialStopLossPercent)}
		}

		mgmt := ManagePosition(ManagementInputs{
			EntryPrice:              t.EntryPrice,
			CurrentPrice:            currentPrice,
			HighestPrice:            state.HighestPrice,
			CurrentStop:             state.CurrentStop,
			MFE:                     state.MFE,
			MAE:                     state.MAE,
			BreakevenTriggerPercent: cfg.BreakevenTriggerPercent,
			TrailingTriggerPercent:  cfg.TrailingTriggerPercent,
			TrailingDistancePercent: cfg.TrailingDistancePercent,
		})
		if mgmt.ShouldExit {
			s.savePositionState(ctx, t.ID, mgmt)
			outcome.Exited, outcome.ExitReason = s.exitPosition(ctx, t, mgmt.ExitReason)
			outcomes = append(outcomes, outcome)
			continue
		}
		s.savePositionState(ctx, t.ID, mgmt)

		// Underlying-exit-confirmation: an additional signal, not a
		// replacement for the hard stop above — only checked if the stop
		// didn't already trigger this tick.
		if !haveDirection {
			direction, directionInputs, err = s.EvaluateDirection(ctx)
			haveDirection = err == nil
		}
		if haveDirection && underlyingLostStructure(t.OptionType, direction.Direction, directionInputs) {
			outcome.Exited, outcome.ExitReason = s.exitPosition(ctx, t, "underlying lost its breakout structure (VWAP side / direction reversed)")
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, nil
}

func (s *Service) savePositionState(ctx context.Context, tradeID int64, mgmt ManagementResult) {
	if err := s.repo.UpsertPositionState(ctx, PositionState{
		TradeID: tradeID, HighestPrice: mgmt.NewHighestPrice, CurrentStop: mgmt.NewStop,
		MFE: mgmt.NewMFE, MAE: mgmt.NewMAE,
	}); err != nil {
		s.log.Error().Err(err).Int64("trade_id", tradeID).Msg("optionsalgo: position state save failed")
	}
}

// currentOptionPrice fetches a fresh price for one held contract via the
// verified Quote-FULL integration — see GetInstrumentExchangeToken's doc
// comment for why this is used instead of paper.Trades' CurrentPrice.
// Returns EffectivePrice (bid-ask midpoint over a possibly-stale LTP), the
// number actually used for stop-loss/trailing decisions.
func (s *Service) currentOptionPrice(ctx context.Context, instrumentID int64) (float64, error) {
	exchange, token, err := s.repo.GetInstrumentExchangeToken(ctx, instrumentID)
	if err != nil {
		return 0, err
	}
	quotes, err := s.angel.GetOptionQuoteFull(ctx, exchange, []string{token})
	if err != nil {
		return 0, err
	}
	if len(quotes) == 0 || quotes[0].LTP <= 0 {
		return 0, fmt.Errorf("no live quote available for instrument %d", instrumentID)
	}
	return quotes[0].EffectivePrice(), nil
}

// underlyingLostStructure reports whether NIFTY has moved against the side
// this position was entered on — e.g. a CE was bought on a BULLISH read, but
// the underlying is no longer BULLISH or has fallen back below VWAP.
func underlyingLostStructure(optionType string, current Direction, in DirectionInputs) bool {
	switch optionType {
	case "CE":
		return current != Bullish || in.Spot < in.VWAP
	case "PE":
		return current != Bearish || in.Spot > in.VWAP
	default:
		return false
	}
}

// exitPosition closes a position at market via the existing ClosePartial
// (qty == full quantity is its "close everything" case), clears the
// trailing state, and logs the exit — P&L and MFE/MAE included — to
// algo_decisions, per the script's explicit exit-logging requirement.
func (s *Service) exitPosition(ctx context.Context, t paper.Trade, reason string) (bool, string) {
	state, _, _ := s.repo.GetPositionState(ctx, t.ID) // best-effort; a zero state just logs zero MFE/MAE

	closed, err := s.paper.ClosePartial(ctx, t.ID, t.Quantity)
	if err != nil {
		s.log.Error().Err(err).Int64("trade_id", t.ID).Msg("optionsalgo: exit failed")
		return false, "exit failed: " + err.Error()
	}
	if err := s.repo.DeletePositionState(ctx, t.ID); err != nil {
		s.log.Error().Err(err).Int64("trade_id", t.ID).Msg("optionsalgo: position state cleanup failed")
	}

	tradeID := t.ID
	pnl := closed.PnL
	var exitPrice *float64
	if closed.ExitPrice != nil {
		exitPrice = closed.ExitPrice
	}
	if err := s.repo.LogDecision(ctx, Decision{
		EvaluatedAt: time.Now().In(market.IST),
		Direction:   "N/A",
		Action:      "EXIT",
		TradeID:     &tradeID,
		ExitPrice:   exitPrice,
		ExitReason:  reason,
		PnL:         &pnl,
		MFE:         &state.MFE,
		MAE:         &state.MAE,
		Detail:      fmt.Sprintf("%s closed: %s", t.Symbol, reason),
	}); err != nil {
		s.log.Error().Err(err).Int64("trade_id", t.ID).Msg("optionsalgo: failed to log exit decision")
	}
	return true, reason
}
