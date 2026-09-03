package investors

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		name string
		want string // Investor.Name, or "" for no match
	}{
		{"Vijay Kedia", "Vijay Kedia"},
		{"vijay   kedia", "Vijay Kedia"},   // case/whitespace-insensitive
		{"Vijay Kedia.", "Vijay Kedia"},    // trailing punctuation
		{"Kedia Securities Private Limited", "Vijay Kedia"}, // alias substring match
		{"Bright Star Investments Pvt Ltd", "Radhakishan Damani"},
		{"RARE Enterprises", "Rakesh Jhunjhunwala"},
		{"Some Unrelated Person", ""},
		{"", ""},
	}
	for _, c := range cases {
		got := match(c.name)
		gotName := ""
		if got != nil {
			gotName = got.Name
		}
		if gotName != c.want {
			t.Errorf("match(%q) = %q, want %q", c.name, gotName, c.want)
		}
	}
}
