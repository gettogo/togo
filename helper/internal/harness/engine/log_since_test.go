package engine

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// An app that kept its cursor across a helper restart asks for items after
// a seq this log never reached. That cursor belongs to the previous epoch,
// so the whole log comes back instead of an empty page with "more".
func TestSinceBeyondTheLogStartsOver(t *testing.T) {
	l := &Log{NewEpoch: func() string { return "e2" }}
	l.Append("s", []harness.Item{{ID: "a", Kind: harness.KindUser}, {ID: "b", Kind: harness.KindAssistant}})
	items, cursor, more := l.Since("s", 900, 0)
	if len(items) != 2 || cursor != 2 || more {
		t.Fatalf("Since(900) = %d items, cursor %d, more %v; want 2, 2, false", len(items), cursor, more)
	}
	if items, _, _ := l.Since("s", 2, 0); len(items) != 0 {
		t.Fatalf("Since(cursor) = %d items, want 0", len(items))
	}
}
