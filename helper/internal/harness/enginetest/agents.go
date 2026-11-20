package enginetest

import (
	// The adapters the rig can serve besides omp and Claude Code (rig.go).
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/aider"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/cline"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/kimi"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/pi"
)

func init() {
	versions["pi"] = "0.73.2"
	versions["kimi"] = "kimi, version 2.5.3"
	versions["cline"] = "3.0.74"
	versions["aider"] = "aider 0.86.1"
}
