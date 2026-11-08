package engine

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/adapters/omp"
)

// The omp file loads as the engine loads it: mappers and runners exist,
// jq compiles and templates parse.
func TestOmpAdapterLoads(t *testing.T) {
	for name, b := range map[string][]byte{"omp": omp.AdapterFile()} {
		a, err := compileAdapter(b, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !a.matches(name, []string{name}) {
			t.Fatalf("%s does not match its own agent", name)
		}
	}
}
