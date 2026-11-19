package bad

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
)

const key = "pi/capability/chat"

func TestB(t *testing.T) {
	coverage.Covers(t, "tape", key)
}
