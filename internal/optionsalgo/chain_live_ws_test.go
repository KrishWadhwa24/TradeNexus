package optionsalgo

import (
	"testing"
	"time"

	"tradenexus/internal/instruments"
)

func TestSameStringSet(t *testing.T) {
	cases := []struct {
		name string
		a, b map[string]bool
		want bool
	}{
		{"both empty", map[string]bool{}, map[string]bool{}, true},
		{"identical", map[string]bool{"NFO:1": true, "NFO:2": true}, map[string]bool{"NFO:1": true, "NFO:2": true}, true},
		{"different size", map[string]bool{"NFO:1": true}, map[string]bool{"NFO:1": true, "NFO:2": true}, false},
		{"same size, different keys", map[string]bool{"NFO:1": true}, map[string]bool{"NFO:2": true}, false},
	}
	for _, c := range cases {
		if got := sameStringSet(c.a, c.b); got != c.want {
			t.Errorf("%s: sameStringSet(%v, %v) = %v, want %v", c.name, c.a, c.b, got, c.want)
		}
	}
}

func TestLiveQuote_NilHubFallsBackToRESTPath(t *testing.T) {
	s := &Service{} // no live hub wired — the nil-Hub path chain_live.go's REST fallback depends on
	strike := 23900.0
	inst := instruments.Instrument{ID: 1, Exchange: "NFO", SymbolToken: "44444", OptionType: "CE", StrikePrice: &strike}
	if _, ok := s.liveQuote(inst); ok {
		t.Fatal("expected liveQuote to report a cache miss when the Service has no live hub")
	}
}

func TestMaxTickAge_IsABackstopNotAFreshnessRequirement(t *testing.T) {
	// Sanity-pins the constant's intent: comfortably longer than the
	// 1-minute evaluation cadence (so a normally-flowing feed is never
	// rejected), but short enough that a frozen websocket can't serve an
	// hours-old price to the entry gate.
	if maxTickAge <= time.Minute {
		t.Errorf("maxTickAge = %v: shorter than the evaluation cadence would reject healthy ticks", maxTickAge)
	}
	if maxTickAge > 10*time.Minute {
		t.Errorf("maxTickAge = %v: too long to catch a stuck feed", maxTickAge)
	}
}
