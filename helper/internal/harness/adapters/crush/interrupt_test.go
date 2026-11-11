package crush

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Crush cancels on the second Escape, which must come as its own write:
// the two in one read as Alt+Escape and only arm the cancel.
func TestInterruptPressesEscapeTwice(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	k := a.Actions["interrupt"].Keys
	if k == nil || strings.Join(k.Keys, " ") != "Escape" || strings.Join(k.Then, " ") != "Escape" || k.Until != spec.UntilIdle {
		t.Fatalf("interrupt %+v", k)
	}
}
