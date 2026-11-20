package tape

import (
	"os"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/testtmp"
)

func TestMain(m *testing.M) {
	// OpenCode 2.x's server is this binary started as a stand-in.
	enginetest.StandIn()
	os.Exit(testtmp.Main(m))
}
