package daemon

// Built-in harness adapters. Each registers its adapter file, and the
// mappers it owns, from its package's init. A new adapter package needs a
// line here, or it never loads.
import (
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/aider"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/amp"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/auggie"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/claude"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/cline"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/codex"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/copilot"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/crush"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/cursor"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/droid"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/goose"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/kilo"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/kimi"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/kiro"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/omp"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/opencode"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/openhands"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/pi"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/shell"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/vibe"
)
