package engine

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/adapters/gemini"
	"github.com/gettogo/togo/helper/internal/harness/adapters/omp"
)

// The omp and Gemini files load as the engine loads them: mappers and
// runners exist, jq compiles and templates parse.
func TestOmpAndGeminiAdaptersLoad(t *testing.T) {
	for name, b := range map[string][]byte{"omp": omp.AdapterFile(), "gemini": gemini.AdapterFile()} {
		a, err := compileAdapter(b, name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !a.matches(name, []string{name}) {
			t.Fatalf("%s does not match its own agent", name)
		}
	}
}
