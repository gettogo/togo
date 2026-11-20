package sessions

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// Claude Code's own screens after each Shift+Tab (2.1.327, live): the
// cycle goes manual, accept edits, plan, auto, and round again. "Don't
// ask" is not in it.
var modeCycle = []struct{ id, screen string }{
	{"default", "manual"}, {"acceptEdits", "acceptedits"}, {"plan", "plan"}, {"auto", "auto"},
}

const modeShots = "harness/adapters/claude/testdata/screens/mode-"

// shiftTab is Claude Code taking Shift+Tab: the pane draws the next
// screen of the cycle. The pane holds the mode screen as a dialog, which
// is how the rig lets a test own what a key does.
type shiftTab struct {
	r       *enginetest.Rig
	mu      sync.Mutex
	at      int
	presses int
}

func newShiftTab(r *enginetest.Rig, at int) *shiftTab {
	s := &shiftTab{r: r, at: at}
	r.Term.OnKey = func(_ *enginetest.Dialog, key string, _ bool) bool {
		if key != "BTab" {
			return false
		}
		s.mu.Lock()
		s.at, s.presses = (s.at+1)%len(modeCycle), s.presses+1
		at := s.at
		s.mu.Unlock()
		r.Term.Open(&enginetest.Dialog{Screen: r.Term.MustCapture(modeShots + modeCycle[at].screen + ".ansi")})
		return true
	}
	r.Term.Open(&enginetest.Dialog{Screen: r.Term.MustCapture(modeShots + modeCycle[at].screen + ".ansi")})
	return s
}

func (s *shiftTab) take() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.presses
	s.presses = 0
	return n
}

// The mode is changed from the phone by pressing Shift+Tab as many times
// as it takes, reading the mode back from the screen after each press,
// against the screens a real Claude Code drew: from manual to plan is two
// presses (through accept edits), plan to auto one, auto round to manual
// one, and a mode already set takes none.
func TestClaudeModeCycle(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "tape", "claude/capability/mode")
	_, l, r := claudeSession(t, time.Hour, nil, enginetest.Options{}, sessiongen.Turn{Say: 30})
	r.SettleUntil(support.FileLimit, enginetest.Has(l.Truth.Last))
	keys := newShiftTab(r, 0)
	for _, step := range []struct {
		to    string
		press int
	}{{"plan", 2}, {"auto", 1}, {"default", 1}, {"acceptEdits", 1}, {"acceptEdits", 0}, {"default", 3}} {
		res, err := r.Act("set_mode", "", map[string]any{"id": step.to})
		if err != nil || res.Result != harness.ResultApplied {
			t.Fatalf("set mode %s: %+v %v", step.to, res, err)
		}
		if got := keys.take(); got != step.press {
			t.Errorf("set mode %s: %d presses of Shift+Tab, want %d", step.to, got, step.press)
		}
		if cur := modeCycle[keys.at].id; cur != step.to {
			t.Errorf("set mode %s: the screen shows %s", step.to, cur)
		}
	}
	// A mode Claude Code does not have is refused before any key is sent.
	if _, err := r.Act("set_mode", "", map[string]any{"id": "yolo"}); err == nil {
		t.Error("an unknown mode was taken")
	}
	if got := keys.take(); got != 0 {
		t.Errorf("%d presses for an unknown mode", got)
	}
	// The phone is told which modes exist, in the words Claude Code uses.
	var labels []string
	for _, m := range r.Describe().Modes {
		labels = append(labels, m.ID+"="+m.Label)
	}
	want := []string{"default=Manual", "acceptEdits=Accept edits", "plan=Plan", "auto=Auto", "dontAsk=Don't ask", "bypassPermissions=Bypass permissions"}
	if !slices.Equal(labels, want) {
		t.Errorf("modes %v, want %v", labels, want)
	}
}

// "Don't ask" is not in Shift+Tab's cycle, so a session whose mode is
// don't ask is changed by restarting Claude Code with --permission-mode,
// on the same conversation. The pane shows the startup screen of a Claude
// Code launched in that mode (a real capture); once the status shows the
// mode the engine read off it, no key is pressed to leave it.
//
// Before the engine has read the screen it does not know the mode, and the
// same request presses Shift+Tab instead (the first press of the real
// cycle leaves don't ask for manual), so this waits for the status.
func TestClaudeModeLeavesDontAskByRestart(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "tape", "claude/capability/mode")
	_, l, r := claudeSession(t, time.Hour, nil, enginetest.Options{}, sessiongen.Turn{Say: 30})
	r.SettleUntil(support.FileLimit, enginetest.Has(l.Truth.Last))
	r.Term.Show(r.Term.MustCapture(modeShots + "dontask.ansi"))
	r.SettleUntil(support.FileLimit, func(st enginetest.State) bool { return support.StatusOf(st).Mode == "dontAsk" })
	res, err := r.Act("set_mode", "", map[string]any{"id": "plan"})
	if err != nil || res.Result != harness.ResultRelaunched {
		t.Fatalf("set mode plan from don't ask: %+v %v", res, err)
	}
	rel := r.Relaunches()
	got := rel[len(rel)-1]
	if got[0] != "claude" || !slices.Contains(got, "--resume") || !containsPair(got, "--permission-mode", "plan") {
		t.Errorf("relaunched %q, want claude --resume <conversation> --permission-mode plan", got)
	}
	if keys := r.Term.KeysSent(); slices.Contains(keys, "BTab") {
		t.Errorf("Shift+Tab was pressed to leave don't ask: %v", keys)
	}
}

func containsPair(argv []string, flag, value string) bool {
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == flag && argv[i+1] == value {
			return true
		}
	}
	return false
}
