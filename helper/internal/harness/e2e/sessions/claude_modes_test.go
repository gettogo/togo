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

// Claude Code started in "don't ask" mode shows it on its first screen,
// and the first Shift+Tab takes it to manual, the start of the cycle (live,
// 2.1.327). With nothing from the hooks yet to say the mode, the mode read
// off the screen is what the phone changes it from: manual to plan from
// there is one press more than the cycle's own two.
func TestClaudeModeLeavesDontAskThroughTheCycle(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "tape", "claude/capability/mode")
	_, l, r := claudeSession(t, time.Hour, nil, enginetest.Options{}, sessiongen.Turn{Say: 30})
	r.SettleUntil(support.FileLimit, enginetest.Has(l.Truth.Last))
	// The pane shows the startup screen; the next press draws manual.
	keys := newShiftTab(r, len(modeCycle)-1)
	r.Term.Open(&enginetest.Dialog{Screen: r.Term.MustCapture(modeShots + "dontask.ansi")})
	res, err := r.Act("set_mode", "", map[string]any{"id": "plan"})
	if err != nil || res.Result != harness.ResultApplied {
		t.Fatalf("set mode plan from don't ask: %+v %v", res, err)
	}
	if got := keys.take(); got != 3 {
		t.Errorf("%d presses of Shift+Tab from don't ask to plan, want 3 (manual, accept edits, plan)", got)
	}
	if len(r.Relaunches()) != 0 {
		t.Errorf("the session was relaunched: %v", r.Relaunches())
	}
}
