// Package version holds the helper's build version.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// Version is set at link time from git describe (see the Makefile build
// target). Builds made without the flag report "dev".
var Version = "dev"

var readBuildInfo = debug.ReadBuildInfo

// Current returns the link-time version. A build without one, such as
// go install github.com/gettogo/togo/helper/cmd/togo@vX.Y.Z, reports the module
// version recorded by the Go toolchain instead.
func Current() string {
	if Version != "dev" {
		return Version
	}
	if info, ok := readBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return Version
}

// String returns the version with the platform, as printed by togo version.
func String() string {
	return fmt.Sprintf("%s (%s/%s)", Current(), runtime.GOOS, runtime.GOARCH)
}
