package sample

import (
	"testing"

	"github.com/gettogo/togo/helper/internal/harness/coverage"
)

func TestA(t *testing.T) {
	coverage.Covers(t, "tape", "pi/capability/chat", "pi/lifecycle/turn_end")
	coverage.Covers(t, "unit", `omp/scale/at_limit`)
	other.Covers(t, "tape", "ignored/capability/x")
}
