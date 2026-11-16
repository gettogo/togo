package droid

import (
	"slices"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	if a.Adapter != "droid" || a.Feeds["hooks"].Mapper != MapperHooks {
		t.Fatalf("unexpected adapter %+v", a.Feeds)
	}
}

// A failed turn's Stop becomes StopFailure; the feed drops what is not listed.
func TestStopFailureIsListed(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(a.Feeds["hooks"].Hook.Events, "StopFailure") {
		t.Fatal("StopFailure is not in the hooks feed's events")
	}
}
