package engine

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

// The command list is read by typing into the terminal on the desk, so a
// list read whole is not read again in the same conversation, however old
// it gets. One cut short, or one that failed, is tried again after
// catalogRetry, and never while a reading runs.
func TestCommandListIsReadOncePerConversation(t *testing.T) {
	now := time.Now()
	rows := []harness.Completion{{Title: "/model"}}
	for _, tc := range []struct {
		name string
		c    terminalCatalog
		at   time.Time
		want bool
	}{
		{"never read", terminalCatalog{}, now, true},
		{"being read", terminalCatalog{reading: true}, now, false},
		{"read whole, a day old", terminalCatalog{rows: rows, at: now, whole: true}, now.Add(24 * time.Hour), false},
		{"cut short, just now", terminalCatalog{rows: rows, at: now}, now.Add(time.Second), false},
		{"cut short, retry due", terminalCatalog{rows: rows, at: now}, now.Add(catalogRetry), true},
		{"failed, just now", terminalCatalog{failed: now}, now.Add(time.Second), false},
		{"failed, retry due", terminalCatalog{failed: now}, now.Add(catalogRetry + time.Second), true},
	} {
		if got := tc.c.due(tc.at); got != tc.want {
			t.Errorf("%s: due = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A list too long to walk within one reading's budget is read from the top
// each time. A second reading that gets no further than the first ends the
// tries: the terminal is not typed into again for it.
func TestACommandListThatNeverFinishesIsNotReadForever(t *testing.T) {
	now := time.Now()
	two := []harness.Completion{{Title: "/model"}, {Title: "/help"}}
	three := append(append([]harness.Completion{}, two...), harness.Completion{Title: "/new"})
	var c terminalCatalog
	c.take(two, false, now)
	if c.whole || !c.due(now.Add(catalogRetry)) {
		t.Fatalf("first cut-short reading: whole %v, want another try", c.whole)
	}
	c.take(three, false, now)
	if c.whole || len(c.rows) != 3 {
		t.Fatalf("a reading that got further: whole %v rows %d, want another try with 3", c.whole, len(c.rows))
	}
	c.take(two, false, now)
	if !c.whole || len(c.rows) != 3 || c.due(now.Add(24*time.Hour)) {
		t.Fatalf("a reading that got no further: whole %v rows %d, want the tries ended and 3 rows kept", c.whole, len(c.rows))
	}
}
