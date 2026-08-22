package promoter

import "testing"

func TestNormalizePersonKey(t *testing.T) {
	cases := map[string]string{
		"ASHISH RAI":   "ASHISH RAI",
		"Ashish Rai":   "ASHISH RAI",
		"  ashish rai": "ASHISH RAI",
	}
	for in, want := range cases {
		if got := normalizePersonKey(in); got != want {
			t.Errorf("normalizePersonKey(%q) = %q, want %q", in, got, want)
		}
	}
	// The two real-world casing variants NSE emits for the same person must
	// collapse to the same key, or they'd wrongly appear as two people.
	if normalizePersonKey("ASHISH RAI") != normalizePersonKey("Ashish Rai") {
		t.Error("casing variant did not collapse to the same person key")
	}
}

func TestRelativeIncrease(t *testing.T) {
	cases := []struct {
		first, latest, want float64
	}{
		{10, 12, 20},
		{0, 5, 0}, // undefined when starting from zero — must not divide by zero
		{5, 5, 0},
		{5, 2.5, -50},
	}
	for _, c := range cases {
		if got := relativeIncrease(c.first, c.latest); got != c.want {
			t.Errorf("relativeIncrease(%v, %v) = %v, want %v", c.first, c.latest, got, c.want)
		}
	}
}
