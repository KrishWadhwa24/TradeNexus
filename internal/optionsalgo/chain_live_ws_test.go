package optionsalgo

import (
	"testing"

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
