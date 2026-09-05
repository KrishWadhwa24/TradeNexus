package optionsalgo

import (
	"context"
	"strings"

	"tradenexus/internal/instruments"
	"tradenexus/internal/live"
)

// ensureChainSubscribed keeps exactly one persistent Hub subscription alive
// for whatever contracts the option chain currently needs, at SnapQuote mode
// (bid/ask/volume/OI, not just LTP — see live.ModeSnapQuote's doc comment).
// The ATM window shifts as spot moves, so the wanted contract set changes
// between calls; when it does, the old subscription is cancelled first
// (freeing the Hub's refcount on anything no longer wanted) before opening a
// new one. A nil Hub (tests that build a Service without one) is a no-op —
// BuildOptionChain then always falls back to REST, same as before this
// change.
func (s *Service) ensureChainSubscribed(ctx context.Context, contracts []instruments.Instrument) {
	if s.live == nil {
		return
	}
	keys := make(map[string]bool, len(contracts))
	for _, c := range contracts {
		keys[chainSubKey(c.Exchange, c.SymbolToken)] = true
	}

	s.chainSubMu.Lock()
	defer s.chainSubMu.Unlock()
	if sameStringSet(s.chainSubKeys, keys) {
		return
	}
	if s.chainSubCancel != nil {
		s.chainSubCancel()
	}
	_, cancel, err := s.live.SubscribeMode(ctx, contracts, live.ModeSnapQuote)
	if err != nil {
		s.log.Warn().Err(err).Msg("option chain: live subscribe failed, BuildOptionChain will use REST only this cycle")
		s.chainSubCancel = nil
		s.chainSubKeys = nil
		return
	}
	s.chainSubCancel = cancel
	s.chainSubKeys = keys
}

func chainSubKey(exchange, token string) string {
	return strings.ToUpper(exchange) + ":" + token
}

func sameStringSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// liveQuote reads a contract's cached tick and reports whether it's usable —
// present in the cache with a real LTP. A cache miss (nothing broadcast for
// this token yet, e.g. it was only just subscribed this cycle) or a zero LTP
// tells the caller to fall back to REST for this one contract instead.
func (s *Service) liveQuote(inst instruments.Instrument) (OptionQuote, bool) {
	if s.live == nil || inst.StrikePrice == nil {
		return OptionQuote{}, false
	}
	tick, ok := s.live.GetLastTick(inst.Exchange, inst.SymbolToken)
	if !ok || tick.Price <= 0 {
		return OptionQuote{}, false
	}
	return OptionQuote{
		InstrumentID:  inst.ID,
		Token:         inst.SymbolToken,
		TradingSymbol: inst.TradingSymbol,
		StrikePrice:   *inst.StrikePrice,
		OptionType:    inst.OptionType,
		LotSize:       inst.LotSize,
		LTP:           tick.Price,
		Bid:           tick.Bid,
		Ask:           tick.Ask,
		Volume:        tick.Volume,
		OpenInterest:  tick.OpenInterest,
	}, true
}
