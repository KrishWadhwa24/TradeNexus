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
	wanted := map[string]instruments.Instrument{key(it.Exchange, it.SymbolToken): it}
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
	data[0] = modeLTP
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
