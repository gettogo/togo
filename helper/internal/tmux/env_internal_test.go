package tmux

import (
	"slices"
	"testing"
)

func TestOutsideTmuxDropsOnlyThePaneVariables(t *testing.T) {
	in := []string{"PATH=/bin", "TMUX=/tmp/tmux-501/togo,1,0", "TMUX_PANE=%3", "TMUX_TMPDIR=/tmp/x", "TMUXINATOR=1", "HOME=/h"}
	want := []string{"PATH=/bin", "TMUX_TMPDIR=/tmp/x", "TMUXINATOR=1", "HOME=/h"}
	if got := outsideTmux(in); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
