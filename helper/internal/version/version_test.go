package version

import (
	"runtime"
	"runtime/debug"
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

func TestVersionFallsBackToModuleVersion(t *testing.T) {
	oldV, oldR := Version, readBuildInfo
	t.Cleanup(func() { Version, readBuildInfo = oldV, oldR })
	set := func(main string, ok bool) {
		readBuildInfo = func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: main}}, ok
		}
	}
	Version = "dev"
	set("v1.4.0", true)
	if got := Current(); got != "v1.4.0" {
		t.Errorf("Current() = %q, want v1.4.0", got)
	}
	if !strings.HasPrefix(String(), "v1.4.0 (") {
		t.Errorf("String() = %q", String())
	}
	set("(devel)", true)
	if got := Current(); got != "dev" {
		t.Errorf("devel build: Current() = %q, want dev", got)
	}
	set("", false)
	if got := Current(); got != "dev" {
		t.Errorf("no build info: Current() = %q, want dev", got)
	}
	Version = "v2.0.0"
	set("v1.4.0", true)
	if got := Current(); got != "v2.0.0" {
		t.Errorf("link-time version must win, got %q", got)
	}
}
