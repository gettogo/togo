package version

import (
	"runtime"
	"strings"
	"testing"
)

func TestStringNamesVersionAndPlatform(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })
	Version = "v1.2.3"
	want := "v1.2.3 (" + runtime.GOOS + "/" + runtime.GOARCH + ")"
	if got := String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if !strings.HasPrefix(String(), "v1.2.3 ") {
		t.Errorf("String() does not start with the version: %q", String())
	}
}
