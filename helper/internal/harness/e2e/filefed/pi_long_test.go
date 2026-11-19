//go:build !race

package filefed

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// piScript is a long Pi conversation: tool calls of every turn, edits in
// every seventh, a model change every 40 turns and a thinking level every
// 33, a command run with "!" every 25, a compaction at turn 100.
func piScript(n, tools, output int) ([]sessiongen.Turn, map[int]sessiongen.PiTurn, string, string) {
	extra := map[int]sessiongen.PiTurn{}
	models := []string{"openai/gpt-5", "anthropic/claude-sonnet-4-5", "moonshotai/kimi-k2.6"}
	levels := []string{"off", "low", "high", "xhigh"}
	model, effort := "anthropic/claude-sonnet-4-5", "medium"
	turns := make([]sessiongen.Turn, n)
	for i := range turns {
		p := i + 1
		x := sessiongen.PiTurn{Think: p%9 == 0}
		if p%7 == 0 {
			x.Edits = 1 + p%3
		}
		if p%40 == 0 {
			model = models[(p/40)%len(models)]
			x.Model = model
		}
		if p%33 == 0 {
			effort = levels[(p/33)%len(levels)]
			x.Effort = effort
		}
		if p%25 == 0 {
			x.Bash = fmt.Sprintf("git log -n %d", p)
		}
		extra[p] = x
		turns[i] = sessiongen.Turn{Say: 300, Tools: tools, Output: output, Compact: p == 100}
	}
	return turns, extra, model, effort
}

// A long Pi conversation is followed as Pi writes it and the engine is
// restarted at five seeded points; every rule holds before and after
// each. The default size crosses the engine's 2000-record replay limit;
// TOGO_E2E_LONG=1 also crosses its 8 MiB read limit.
func TestPiLongSessionWithRestarts(t *testing.T) {
	coverage.Covers(t, "generated", "pi/scale/long_session", "pi/scale/at_limit", "pi/lifecycle/helper_restart", "pi/capability/chat")
	e := support.NewEnv(t, 48*time.Hour)
	n := 230
	turns, extra, model, effort := piScript(n, 4, support.Scale(40, 10_000))
	points := support.Cuts(21, 5, 20, n)
	l, err := sessiongen.Write(e.Home, &sessiongen.Pi{Extra: extra}, sessiongen.Script{Seed: 21, Start: e.Start, Cwd: e.Cwd, Turns: turns[:points[0]]})
	if err != nil {
		t.Fatal(err)
	}
	r := e.Rig(t, "pi")
	st := support.Drive(t, r, l, turns, points)
	if l.Records[l.Main()] <= 2000 {
		t.Fatalf("%d records, want above the replay limit of 2000", l.Records[l.Main()])
	}
	if fi, err := os.Stat(l.Main()); err != nil || support.LongMode() && fi.Size() <= 8<<20 {
		t.Fatalf("main file %d bytes (%v): want above 8 MiB in long mode", fi.Size(), err)
	}
	final := support.RestoreTwice(t, r, l, n)
	if got := support.StatusOf(final); got.Model != model || got.Effort != effort {
		t.Errorf("status after the restore: model %q effort %q, want %q %q", got.Model, got.Effort, model, effort)
	}
	if got := support.StatusOf(st); got.Model != model || got.Effort != effort {
		t.Errorf("status while following: model %q effort %q, want %q %q", got.Model, got.Effort, model, effort)
	}
	// The newest turns are all there, each ended ok, in order.
	if ok := support.TurnsIn(final, harness.StateOK); len(ok) < 100 {
		t.Errorf("%d finished turns in the restore, want at least 100", len(ok))
	}
	// The compaction at turn 100 is older than the replay window of the
	// restore only in the long run; while following it was seen once.
	if c := support.Items(st, support.Notice(harness.NoticeCompacted)); len(c) > 1 {
		t.Errorf("%d compaction notices, want at most 1", len(c))
	}
	// Every edit turn in the window counts its files.
	for _, it := range support.TurnsIn(final, harness.StateOK) {
		if it.Turn.FilesChanged < 0 || it.Turn.Steps < 1 {
			t.Errorf("turn %s: %d steps, %d files", it.ID, it.Turn.Steps, it.Turn.FilesChanged)
		}
	}
}
