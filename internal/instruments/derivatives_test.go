package instruments

import "testing"

func TestOptionTypeSuffix(t *testing.T) {
	cases := map[string]string{
		"NIFTY08SEP2622400PE": "PE",
		"NIFTY08SEP2622400CE": "CE",
		"Nifty 50":            "",
		"RELIANCE-EQ":         "",
	}
	for in, want := range cases {
		if got := optionTypeSuffix(in); got != want {
			t.Errorf("optionTypeSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}
