package optionsalgo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"tradenexus/internal/angel"
	"tradenexus/internal/instruments"
)

// niftyUnderlying is the one underlying this algo trades — see the plan's
// "SENSEX considered and deferred" note (Angel's Option Greeks API doesn't
// cover SENSEX, and delta selection is load-bearing for strike selection).
const niftyUnderlying = "NIFTY"

// BuildOptionChain resolves the nearest-expiry NIFTY chain around spot (ATM
// +/- strikesEachSide strikes, both CE/PE) and joins it with live
// bid/ask/volume/OI (GetOptionQuoteFull) and Greeks (GetOptionGreeks) —
// exactly the two Angel integrations verified live in Phase 0. Both calls
// are batched (one quote-full call for every token, one Greeks call for the
// whole expiry) rather than per-contract, since Angel already supports that
// and it's far cheaper against the rate limiter.
func (s *Service) BuildOptionChain(ctx context.Context, spot float64) ([]OptionQuote, error) {
	cfg, err := s.repo.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	expiry, err := s.repo.NearestOptionExpiry(ctx, niftyUnderlying)
	if err != nil {
		return nil, err
	}
	strikes, err := s.repo.StrikesForExpiry(ctx, niftyUnderlying, expiry)
	if err != nil {
		return nil, err
	}
	picked := NearestATMStrikes(strikes, spot, cfg.StrikesEachSide)
	if len(picked) == 0 {
		return nil, fmt.Errorf("no strikes available for %s expiry %s", niftyUnderlying, expiry.Format("2006-01-02"))
	}
	contracts, err := s.repo.OptionContractsForStrikes(ctx, niftyUnderlying, expiry, picked)
	if err != nil {
		return nil, err
	}
	if len(contracts) == 0 {
		return nil, fmt.Errorf("no option contracts found for the selected strikes (expiry %s)", expiry.Format("2006-01-02"))
	}

	byToken := make(map[string]instruments.Instrument, len(contracts))
	tokens := make([]string, 0, len(contracts))
	exchange := contracts[0].Exchange
	for _, c := range contracts {
		byToken[c.SymbolToken] = c
		tokens = append(tokens, c.SymbolToken)
	}

	quotes, err := s.angel.GetOptionQuoteFull(ctx, exchange, tokens)
	if err != nil {
		return nil, fmt.Errorf("quote-full: %w", err)
	}
	greeks, err := s.angel.GetOptionGreeks(ctx, niftyUnderlying, angelExpiryFormat(expiry))
	if err != nil {
		return nil, fmt.Errorf("option greeks: %w", err)
	}
	greeksByStrikeType := make(map[string]angel.OptionGreek, len(greeks))
	for _, g := range greeks {
		greeksByStrikeType[greekKey(g.StrikePrice, g.OptionType)] = g
	}

	out := make([]OptionQuote, 0, len(quotes))
	for _, q := range quotes {
		inst, ok := byToken[q.SymbolToken]
		if !ok || inst.StrikePrice == nil {
			continue
		}
		oq := OptionQuote{
			InstrumentID:  inst.ID,
			Token:         inst.SymbolToken,
			TradingSymbol: inst.TradingSymbol,
			StrikePrice:   *inst.StrikePrice,
			OptionType:    inst.OptionType,
			LotSize:       inst.LotSize,
			LTP:           q.LTP,
			Bid:           q.Bid(),
			Ask:           q.Ask(),
			Volume:        q.TradeVolume,
			OpenInterest:  q.OpenInterest,
		}
		if g, ok := greeksByStrikeType[greekKey(*inst.StrikePrice, inst.OptionType)]; ok {
			oq.Delta, oq.Gamma, oq.Theta, oq.Vega, oq.IV = g.Delta, g.Gamma, g.Theta, g.Vega, g.IV
		}
		out = append(out, oq)
	}
	return out, nil
}

// SelectContract narrows a built chain to one side (CE for bullish, PE for
// bearish per the script — this is direction-gated, never both), applies the
// delta-band selection, then the liquidity filter. reason explains the
// outcome either way, for the decision log (Phase 5).
func (s *Service) SelectContract(ctx context.Context, direction Direction, chain []OptionQuote) (OptionQuote, string, bool) {
	cfg, err := s.repo.GetConfig(ctx)
	if err != nil {
		return OptionQuote{}, "config: " + err.Error(), false
	}

	var side string
	switch direction {
	case Bullish:
		side = "CE"
	case Bearish:
		side = "PE"
	default:
		return OptionQuote{}, "no direction — nothing to select", false
	}

	var sideQuotes []OptionQuote
	for _, q := range chain {
		if q.OptionType == side {
			sideQuotes = append(sideQuotes, q)
		}
	}
	if len(sideQuotes) == 0 {
		return OptionQuote{}, fmt.Sprintf("no %s contracts in the built chain", side), false
	}

	selected, ok := SelectByDelta(sideQuotes, cfg.DeltaTarget, cfg.DeltaMin, cfg.DeltaMax)
	if !ok {
		return OptionQuote{}, fmt.Sprintf("no %s strike with delta in [%.2f,%.2f]", side, cfg.DeltaMin, cfg.DeltaMax), false
	}

	avgVol := AverageVolume(sideQuotes)
	if liquid, why := LiquidityCheck(selected, avgVol, cfg.MaxSpreadPercent, cfg.MinVolumeMultiplier); !liquid {
		return OptionQuote{}, fmt.Sprintf("%s rejected: %s", selected.TradingSymbol, why), false
	}

	return selected, fmt.Sprintf("%s selected: delta=%.3f closest to target %.2f, spread=%.2f%%", selected.TradingSymbol, selected.Delta, cfg.DeltaTarget, selected.SpreadPercent()), true
}

// angelExpiryFormat matches GetOptionGreeks' expected "02Jan2006"-style
// upper-cased format (e.g. "08SEP2026") — verified live in Phase 0.
func angelExpiryFormat(t time.Time) string {
	return strings.ToUpper(t.Format("02Jan2006"))
}

func greekKey(strike float64, optionType string) string {
	return fmt.Sprintf("%.2f-%s", strike, optionType)
}
