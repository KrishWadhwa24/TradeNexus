package live

import (
	"encoding/binary"
	"testing"

	"github.com/rs/zerolog"

	"tradenexus/internal/instruments"
)

func TestHubSubscriptionRefCounts(t *testing.T) {
	h := NewHub(nil, zerolog.Nop())
	it := instruments.Instrument{
		ID:            10,
		Exchange:      "NSE",
		SymbolToken:   "3045",
		TradingSymbol: "SBIN-EQ",
	}
	wanted := map[string]clientWant{key(it.Exchange, it.SymbolToken): {instrument: it, mode: ModeLTP}}
	ch1 := make(chan Tick, 1)
	ch2 := make(chan Tick, 1)

	first := h.addClient(ch1, wanted)
	if len(first) != 1 {
		t.Fatalf("expected first subscriber to subscribe once, got %d", len(first))
	}
	second := h.addClient(ch2, wanted)
	if len(second) != 0 {
		t.Fatalf("expected duplicate subscriber to reuse pending subscription, got %d", len(second))
	}

	h.markSubscribeResult(first, true)
	sub := h.subs[key(it.Exchange, it.SymbolToken)]
	if sub == nil || sub.refs != 2 || !sub.subscribed || sub.pending {
		t.Fatalf("bad subscription state after subscribe: %+v", sub)
	}

	h.removeClient(ch1, wanted)
	sub = h.subs[key(it.Exchange, it.SymbolToken)]
	if sub == nil || sub.refs != 1 {
		t.Fatalf("expected one remaining ref, got %+v", sub)
	}
	if _, ok := <-ch1; ok {
		t.Fatal("expected first client channel to be closed")
	}

	h.removeClient(ch2, wanted)
	if _, ok := h.subs[key(it.Exchange, it.SymbolToken)]; ok {
		t.Fatal("expected subscription to be pruned after last client leaves")
	}
}

func TestParseTickUsesActiveSubscription(t *testing.T) {
	h := NewHub(nil, zerolog.Nop())
	it := instruments.Instrument{
		ID:            10,
		Exchange:      "NSE",
		SymbolToken:   "3045",
		TradingSymbol: "SBIN-EQ",
	}
	h.subs[key(it.Exchange, it.SymbolToken)] = &subscription{
		instrument: it,
		refs:       1,
		subscribed: true,
	}

	data := make([]byte, 51)
	data[0] = ModeLTP
	data[1] = byte(exchangeType("NSE"))
	copy(data[2:27], []byte(it.SymbolToken))
	binary.LittleEndian.PutUint64(data[43:51], 12345)

	tick, ok := h.parseTick(data)
	if !ok {
		t.Fatal("expected tick to parse")
	}
	if tick.InstrumentID != it.ID || tick.Symbol != it.TradingSymbol || tick.Price != 123.45 {
		t.Fatalf("unexpected tick: %+v", tick)
	}
}

func TestParseModeSnapQuoteParsesBidAskVolumeOI(t *testing.T) {
	h := NewHub(nil, zerolog.Nop())
	it := instruments.Instrument{
		ID:            61796,
		Exchange:      "NFO",
		SymbolToken:   "44444",
		TradingSymbol: "NIFTY15SEP2622350CE",
	}
	h.subs[key(it.Exchange, it.SymbolToken)] = &subscription{
		instrument: it,
		refs:       1,
		subscribed: true,
		mode:       ModeSnapQuote,
	}

	const depthStart, depthRecords, depthRecSize = 147, 10, 20
	data := make([]byte, depthStart+depthRecords*depthRecSize)
	data[0] = ModeSnapQuote
	data[1] = byte(exchangeType("NFO"))
	copy(data[2:27], []byte(it.SymbolToken))
	binary.LittleEndian.PutUint64(data[43:51], 158160)   // LTP 1581.60
	binary.LittleEndian.PutUint64(data[67:75], 9000)     // volume
	binary.LittleEndian.PutUint64(data[131:139], 540000) // OI

	// record 0: best buy (bid) at 1370.70
	binary.LittleEndian.PutUint16(data[depthStart:depthStart+2], 0)
	binary.LittleEndian.PutUint64(data[depthStart+10:depthStart+18], 137070)
	// record 5: best sell (ask) at 1792.50
	sellOff := depthStart + 5*depthRecSize
	binary.LittleEndian.PutUint16(data[sellOff:sellOff+2], 1)
	binary.LittleEndian.PutUint64(data[sellOff+10:sellOff+18], 179250)

	tick, ok := h.parseTick(data)
	if !ok {
		t.Fatal("expected mode-3 tick to parse")
	}
	if tick.Price != 1581.60 {
		t.Fatalf("expected LTP 1581.60, got %v", tick.Price)
	}
	if tick.Bid != 1370.70 || tick.Ask != 1792.50 {
		t.Fatalf("expected bid/ask 1370.70/1792.50, got %v/%v", tick.Bid, tick.Ask)
	}
	if tick.Volume != 9000 {
		t.Fatalf("expected volume 9000, got %v", tick.Volume)
	}
	if tick.OpenInterest != 540000 {
		t.Fatalf("expected OI 540000, got %v", tick.OpenInterest)
	}
}

func TestRemoveClientPrunesLastTickOnFullUnsubscribe(t *testing.T) {
	h := NewHub(nil, zerolog.Nop())
	it := instruments.Instrument{
		ID:            61796,
		Exchange:      "NFO",
		SymbolToken:   "44444",
		TradingSymbol: "NIFTY15SEP2622350CE",
	}
	k := key(it.Exchange, it.SymbolToken)
	wanted := map[string]clientWant{k: {instrument: it, mode: ModeSnapQuote}}
	ch := make(chan Tick, 1)
	first := h.addClient(ch, wanted)
	h.markSubscribeResult(first, true)

	h.lastTickMu.Lock()
	h.lastTicks[k] = Tick{InstrumentID: it.ID, Price: 1581.60}
	h.lastTickMu.Unlock()

	h.removeClient(ch, wanted) // last (only) ref leaves — refs drops to 0

	if _, ok := h.GetLastTick(it.Exchange, it.SymbolToken); ok {
		t.Fatal("expected the cached tick to be pruned once the token is fully unsubscribed — a later resubscribe must not see this stale price as \"live\"")
	}
}

func TestMarkSubscribeResultReturnsFollowUpWhenModeRaisedDuringFlight(t *testing.T) {
	h := NewHub(nil, zerolog.Nop())
	it := instruments.Instrument{
		ID:            61796,
		Exchange:      "NFO",
		SymbolToken:   "44444",
		TradingSymbol: "NIFTY15SEP2622350CE",
	}
	k := key(it.Exchange, it.SymbolToken)

	// Caller A wants LTP for a brand-new token — addClient returns a
	// send-list snapshotting mode=LTP.
	ltpWant := map[string]clientWant{k: {instrument: it, mode: ModeLTP}}
	sentA := h.addClient(make(chan Tick, 1), ltpWant)
	if len(sentA) != 1 || sentA[0].mode != ModeLTP {
		t.Fatalf("expected caller A's send-list to snapshot ModeLTP, got %+v", sentA)
	}

	// While A's send is still "in flight" (not yet marked), caller B races
	// in wanting SnapQuote for the same token. Since the token isn't marked
	// subscribed yet, B's addClient bumps sub.mode but produces no send of
	// its own (needsSend stays false — see addClient's doc comment).
	snapWant := map[string]clientWant{k: {instrument: it, mode: ModeSnapQuote}}
	sentB := h.addClient(make(chan Tick, 1), snapWant)
	if len(sentB) != 0 {
		t.Fatalf("expected caller B to produce no send while A's subscribe is still pending, got %+v", sentB)
	}
	if h.subs[k].mode != ModeSnapQuote {
		t.Fatalf("expected sub.mode raised to ModeSnapQuote by caller B, got %d", h.subs[k].mode)
	}

	// A's send "completes" using its stale mode=LTP snapshot. Without the
	// fix, this would leave the wire stuck at LTP forever even though
	// sub.mode says SnapQuote.
	followUp := h.markSubscribeResult(sentA, true)
	if len(followUp) != 1 || followUp[0].mode != ModeSnapQuote {
		t.Fatalf("expected a follow-up re-send at ModeSnapQuote, got %+v", followUp)
	}
	if !h.subs[k].pending {
		t.Fatal("expected sub.pending to stay true until the follow-up send resolves it")
	}
}

func TestSubscribeModeUpgradesExistingSubscriptionToHigherMode(t *testing.T) {
	h := NewHub(nil, zerolog.Nop())
	it := instruments.Instrument{
		ID:            61796,
		Exchange:      "NFO",
		SymbolToken:   "44444",
		TradingSymbol: "NIFTY15SEP2622350CE",
	}
	k := key(it.Exchange, it.SymbolToken)

	ltpWant := map[string]clientWant{k: {instrument: it, mode: ModeLTP}}
	ch1 := make(chan Tick, 1)
	first := h.addClient(ch1, ltpWant)
	h.markSubscribeResult(first, true)
	if h.subs[k].mode != ModeLTP {
		t.Fatalf("expected initial mode ModeLTP, got %d", h.subs[k].mode)
	}

	snapWant := map[string]clientWant{k: {instrument: it, mode: ModeSnapQuote}}
	ch2 := make(chan Tick, 1)
	second := h.addClient(ch2, snapWant)
	if len(second) != 1 || second[0].mode != ModeSnapQuote {
		t.Fatalf("expected an upgrade re-send at ModeSnapQuote, got %+v", second)
	}
	if h.subs[k].mode != ModeSnapQuote || h.subs[k].refs != 2 {
		t.Fatalf("expected mode upgraded to SnapQuote with 2 refs, got %+v", h.subs[k])
	}

	h.markSubscribeResult(second, true)

	// The lower-mode client leaving does not downgrade the token — see
	// subscription.mode's doc comment.
	h.removeClient(ch1, ltpWant)
	if h.subs[k].mode != ModeSnapQuote || h.subs[k].refs != 1 {
		t.Fatalf("expected mode to stay SnapQuote after ModeLTP client left, got %+v", h.subs[k])
	}
}
