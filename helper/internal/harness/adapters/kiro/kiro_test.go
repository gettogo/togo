package kiro

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	if a.Adapter != "kiro" {
		t.Fatalf("adapter %q", a.Adapter)
	}
}
