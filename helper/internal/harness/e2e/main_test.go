package e2e

import (
	"os"
	"testing"

	"github.com/gettogo/togo/helper/internal/testtmp"
)

// The run's temporary folders live under one folder that the run removes,
// and the next run removes if this one is killed.
func TestMain(m *testing.M) { os.Exit(testtmp.Main(m)) }
