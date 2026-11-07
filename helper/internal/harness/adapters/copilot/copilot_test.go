package copilot

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	// The hook feed filters on the names `togo hook copilot` forwards.
	d, _ := hookstyle.For("copilot")
	want := map[string]bool{}
	for _, c := range d.Events {
		want[c] = true
	}
	for _, ev := range a.Feeds["hooks"].Hook.Events {
		if !want[ev] {
			t.Errorf("feed lists %q, which no copilot hook maps to", ev)
		}
	}
	if len(a.Feeds["hooks"].Hook.Events) != len(want) {
		t.Errorf("feed lists %d events, copilot maps %d", len(a.Feeds["hooks"].Hook.Events), len(want))
	}
}
