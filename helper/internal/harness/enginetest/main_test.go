package enginetest

import (
	"os"
	"testing"

	"github.com/gettogo/togo/helper/internal/testtmp"
)

// The run's temporary folders live under one folder that the run removes,
// and the next run removes if this one is killed.
// A run that the engine started as an OpenCode 2.x server stand-in is
// that and nothing else (standin.go).
func TestMain(m *testing.M) {
	StandIn()
	os.Exit(testtmp.Main(m))
}
