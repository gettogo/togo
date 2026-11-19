//go:build !race

package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// clineScript is a long Cline conversation: tool calls in every turn, an
// editor call every seventh, a question every seventeenth (answered, then
// cancelled in turn), a thinking block every ninth and a mode change every
// 25.
func clineScript(n, tools, output int) ([]sessiongen.Turn, map[int]sessiongen.ClineTurn) {
	extra := map[int]sessiongen.ClineTurn{}
	turns := make([]sessiongen.Turn, n)
	for i := range turns {
		p := i + 1
		x := sessiongen.ClineTurn{Think: p%9 == 0}
		if p%7 == 0 {
			x.Edits = 1
		}
		if p%17 == 0 {
			x.Question = &sessiongen.ClineQuestion{Question: fmt.Sprintf("Which for step %d?", p), Options: []string{"A", "B"}, Answer: "B", Cancelled: p%34 == 0}
		}
		if p%25 == 0 {
			x.Mode = []string{"plan", "act"}[(p/25)%2]
		}
		extra[p] = x
		turns[i] = sessiongen.Turn{Say: 300, Tools: tools, Output: output}
	}
	return turns, extra
}

// A long Cline conversation is followed as Cline rewrites its document and
// the engine is restarted at five seeded points; every rule holds before
// and after each. The default size crosses the engine's log of 2000 items;
// TOGO_E2E_LONG=1 grows the document to just under its 8 MiB read limit
// (above it is TestKnownFault_ClineDocumentOver8MiB).
func TestClineLongSessionWithRestarts(t *testing.T) {
	coverage.Covers(t, "generated", "cline/scale/long_session", "cline/scale/at_limit", "cline/lifecycle/helper_restart", "cline/capability/chat")
	e := newEnv(t, 48*time.Hour)
	n := 300
	turns, extra := clineScript(n, 4, scale(40, 3_000))
	points := cuts(23, 5, 20, n)
	l, err := sessiongen.Write(e.home, &sessiongen.Cline{Extra: extra}, sessiongen.Script{Seed: 23, Start: e.start, Cwd: e.cwd, Turns: turns[:points[0]]})
	if err != nil {
		t.Fatal(err)
	}
	r := e.rig(t, "cline")
	st := drive(t, r, l, turns, points)
	if l.Records[l.Main()] < 2000 {
		t.Fatalf("%d messages, want at least the 2000 items of the log", l.Records[l.Main()])
	}
	last := restoreTwice(t, r, l, n)
	if got := len(userPrompts(last)); got < 150 {
		t.Errorf("%d prompts in the restore, want at least 150", got)
	}
	if len(st.Items) > 2000 || len(last.Items) > 2000 {
		t.Errorf("%d and %d items: the log holds at most 2000", len(st.Items), len(last.Items))
	}
	if ok := turnsIn(last, harness.StateOK); len(ok) < 150 {
		t.Errorf("%d finished turns in the restore, want at least 150", len(ok))
	}
	// A question every 17th turn: all answered or cancelled, none waiting.
	for _, it := range items(last, ofKind(harness.KindQuestion)) {
		if len(it.Question.Answered) == 0 {
			t.Errorf("question %s is still open", it.ID)
		}
	}
	if last.Mission.Summary.NeedsYou != 0 {
		t.Errorf("needs you %d", last.Mission.Summary.NeedsYou)
	}
}
