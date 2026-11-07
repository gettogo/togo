package daemon

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness"
)

// Every adapter file shipped under harness/adapters is registered by the
// daemon's imports.
func TestEveryAdapterRegistered(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "harness", "adapters", "*", "*.yaml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no adapter files found: %v", err)
	}
	have := harness.AdapterFiles()
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".yaml")
		if _, ok := have[id]; !ok {
			t.Errorf("adapter %s (%s) is not imported by the daemon", id, f)
		}
	}
}
