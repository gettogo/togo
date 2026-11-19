package generic

import (
	"sync"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
)

var (
	cellsOnce sync.Once
	cells     map[string]coverage.Cell
)

// declare says whether a scenario applies to the agent, and declares the
// cell it proves. A cell that does not apply (the adapter declares no such
// capability) is skipped with the reason the inventory gives. One that
// applies runs whether or not it is declared below: a new profile runs
// every scenario before its cells are claimed.
func declare(t *testing.T, agent, cell string) bool {
	t.Helper()
	cellsOnce.Do(func() {
		list, err := coverage.Cells()
		if err != nil {
			t.Fatalf("coverage: %v", err)
		}
		cells = coverage.Index(list)
	})
	key := agent + "/" + cell
	c, ok := cells[key]
	switch {
	case !ok:
		t.Skipf("%s is not a cell of the inventory", key)
	case c.NotApplicable != "":
		t.Skipf("%s does not apply: %s", key, c.NotApplicable)
	}
	if d, ok := declared[key]; ok {
		d(t)
	}
	return true
}

// declared holds, per cell, the literal declaration the coverage scan
// reads (coverage.ScanEvidence takes only string literals).
var declared = map[string]func(t *testing.T){}
