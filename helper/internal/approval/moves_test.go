package approval

import "testing"

// The confirm key waits for the cursor only when the keys before it move
// the cursor. A digit and Enter (Kimi) go together: the digit never shows
// as a selected option to wait for.
func TestOnlyCursorMovesAreSplitFromTheConfirmKey(t *testing.T) {
	for _, tc := range []struct {
		keys []string
		want bool
	}{
		{[]string{"Down", "Down"}, true},
		{[]string{"Left"}, true},
		{[]string{"2"}, false},
		{[]string{"Down", "2"}, false},
		{nil, false},
	} {
		if got := movesOnly(tc.keys); got != tc.want {
			t.Errorf("movesOnly(%v) = %v, want %v", tc.keys, got, tc.want)
		}
	}
}
