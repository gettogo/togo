package coverage

import (
	"fmt"
	"sync"
	"testing"
)

// Tiers of evidence.
const (
	TierLive      = "live"      // a real agent, in the nightly run
	TierTape      = "tape"      // real recorded input replayed through the engine
	TierGenerated = "generated" // a session generator
	TierUnit      = "unit"      // a unit test
)

// Tiers lists the valid tiers.
var Tiers = []string{TierLive, TierTape, TierGenerated, TierUnit}

// Satisfies reports whether evidence of this tier covers a cell on its own.
func Satisfies(tier string) bool { return tier == TierTape || tier == TierGenerated }

func validTier(t string) bool {
	for _, v := range Tiers {
		if v == t {
			return true
		}
	}
	return false
}

var (
	realOnce  sync.Once
	realCells map[string]Cell
	realErr   error
)

func loadReal() (map[string]Cell, error) {
	realOnce.Do(func() {
		cells, err := Cells()
		if err != nil {
			realErr = err
			return
		}
		realCells = Index(cells)
	})
	return realCells, realErr
}

// Covers declares, at the top of a test, which cells the test exercises and
// with what tier of evidence. It does nothing at run time except check the
// declaration: an unknown or not-applicable key, or a bad tier, fails the
// test, so a typo or a removed cell is caught where it was written. The
// inventory of declarations is read from the source (ScanEvidence), so the
// arguments must be string literals.
func Covers(t testing.TB, tier string, keys ...string) {
	t.Helper()
	if !validTier(tier) {
		t.Fatalf("coverage.Covers: unknown tier %q (want one of %v)", tier, Tiers)
	}
	if len(keys) == 0 {
		t.Fatalf("coverage.Covers: no cell keys given")
	}
	if HelperDir() == "" {
		return // no source tree to check against; the ratchet test checks it
	}
	cells, err := loadReal()
	if err != nil {
		t.Fatalf("coverage.Covers: %v", err)
	}
	for _, k := range keys {
		c, ok := cells[k]
		switch {
		case !ok:
			t.Fatalf("coverage.Covers: %q is not a cell (a typo, or the adapter no longer declares it)", k)
		case c.NotApplicable != "":
			t.Fatalf("%s", fmt.Sprintf("coverage.Covers: %q is not applicable: %s", k, c.NotApplicable))
		}
	}
}
