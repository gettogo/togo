package crush

import (
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// Crush cancels on the second Escape.
func TestInterruptPressesEscapeTwice(t *testing.T) {
	a, err := spec.Parse(AdapterFile())
	if err != nil {
		t.Fatal(err)
	}
	if k := a.Actions["interrupt"].Keys; k == nil || strings.Join(k.Keys, " ") != "Escape Escape" {
		t.Fatalf("interrupt %+v", k)
	}
}
