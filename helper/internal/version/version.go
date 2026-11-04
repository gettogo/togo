// Package version holds the helper's build version.
package version

import (
	"fmt"
	"runtime"
)

// Version is set at link time from git describe (see the Makefile build
// target). Builds made without the flag report "dev".
var Version = "dev"

// String returns the version with the platform, as printed by togo version.
func String() string {
	return fmt.Sprintf("%s (%s/%s)", Version, runtime.GOOS, runtime.GOARCH)
}
