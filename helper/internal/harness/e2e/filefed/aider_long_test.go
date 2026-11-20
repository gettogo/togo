//go:build !race

package filefed

import (
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// aiderScript is a long Aider conversation: files added to the chat every
// fifth turn, edits and a commit every seventh, shell commands every
// third.
func aiderScript(n, say int) ([]sessiongen.Turn, map[int]sessiongen.AiderTurn) {
	extra := map[int]sessiongen.AiderTurn{}
	turns := make([]sessiongen.Turn, n)
	for i := range turns {
		p := i + 1
		x := sessiongen.AiderTurn{}
		if p%5 == 0 {
			x.Files = 1 + p%3
		}
		if p%7 == 0 {
			x.Edits, x.Commit = 1+p%3, p%2 == 0
		}
		extra[p] = x
		t := sessiongen.Turn{Say: say}
		if p%3 == 0 {
			t.Tools = 1 + p%2
		}
		turns[i] = t
	}
	return turns, extra
}

// A long Aider history is followed as Aider appends to it and the engine
// is restarted at five seeded points; every rule holds before and after
// each. The default size crosses the engine's log of 2000 items.
func TestAiderLongSessionWithRestarts(t *testing.T) {
	t.Parallel()
	support.Heavy(t)
	coverage.Covers(t, "generated", "aider/scale/long_session", "aider/scale/at_limit", "aider/lifecycle/helper_restart", "aider/capability/chat")
	e := support.NewEnv(t, 48*time.Hour)
	n := 450
	turns, extra := aiderScript(n, support.Scale(300, 3_000))
	points := support.Cuts(24, 5, 20, n)
	l, err := sessiongen.Write(e.Home, &sessiongen.Aider{Extra: extra}, sessiongen.Script{Seed: 24, Start: e.Start, Cwd: e.Cwd, Turns: turns[:points[0]]})
	if err != nil {
		t.Fatal(err)
	}
	r := e.Rig(t, "aider")
	st := support.Drive(t, r, l, turns, points)
	last := support.RestoreTwice(t, r, l, n)
	if len(st.Items) > 2000 || len(last.Items) > 2000 {
		t.Errorf("%d and %d items: the log holds at most 2000", len(st.Items), len(last.Items))
	}
	if ok := support.TurnsIn(last, harness.StateOK); len(ok) < 150 {
		t.Errorf("%d finished turns in the restore, want at least 150", len(ok))
	}
}
