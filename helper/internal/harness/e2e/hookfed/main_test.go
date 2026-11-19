package hookfed

import (
	"os"
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
)

func TestMain(m *testing.M) { os.Exit(support.Main(m)) }
