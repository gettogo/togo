package engine

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"

	_ "github.com/gettogo/togo/helper/internal/harness/adapters/aider"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/amp"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/cline"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/copilot"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/cursor"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/droid"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/goose"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/kiro"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/openhands"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/vibe"
)

// Every built-in adapter file compiles: mappers and runners exist, jq and
// map expressions parse, the version range reads.
func TestMoreAdapterFilesCompile(t *testing.T) {
	files := harness.AdapterFiles()
	for _, id := range []string{"droid", "vibe", "copilot", "goose", "aider", "cline", "amp", "cursor", "kiro", "openhands"} {
		b, ok := files[id]
		if !ok {
			t.Errorf("%s: not registered", id)
			continue
		}
		if _, err := compileAdapter(b, id); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
}
