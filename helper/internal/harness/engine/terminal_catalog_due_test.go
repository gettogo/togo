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
