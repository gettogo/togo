package enginetest

// The adapters of the agents that keep their chat in files and databases
// beside the hook-driven ones, and the adapters of the command-line
// agents that have no chat, which the terminal row of the matrix stands
// for. A rig serves them with the fake program below, whose --version
// satisfies each adapter's version gate.
import (
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/auggie"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/crush"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/goose"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/kiro"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/openhands"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/shell"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/vibe"
)

func init() {
	versions["vibe"] = "vibe 3.1.0"
	versions["goose"] = "goose 1.57.0"
	versions["crush"] = "crush version v0.100.0"
	versions["auggie"] = "1.2.0"
	versions["kiro"] = "1.2.0"
	programs["kiro"] = "kiro-cli"
	versions["openhands"] = "1.21.0"
}
