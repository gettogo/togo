package engine

import (
	"regexp"
	"strings"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

// modeScreen reads the mode off the pane with an adapter's modes.screen
// rules, for harnesses that report it nowhere else (Crush's YOLO MODE
// footer, Aider's "ask> " prompt). It joins the mappers that read the
// screen when the session starts and each time a turn ends.
type modeScreen struct {
	rules []modeRule
	dflt  string
}

type modeRule struct {
	re *regexp.Regexp
	id string
}

func newModeScreen(m *spec.Modes) *modeScreen {
	if m == nil || len(m.Screen) == 0 {
		return nil
	}
	ms := &modeScreen{dflt: m.ScreenDefault}
	for _, r := range m.Screen {
		if re, err := regexp.Compile(r.Line); err == nil {
			ms.rules = append(ms.rules, modeRule{re: re, id: r.ID})
		}
	}
	return ms
}

// ReadScreen returns the mode named by the last line any rule matches.
func (m *modeScreen) ReadScreen(text string) map[string]string {
	lines := strings.Split(ansiCSI.ReplaceAllString(text, ""), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimRight(lines[i], " \t\r")
		for _, r := range m.rules {
			if r.re.MatchString(l) {
				return map[string]string{"mode": r.id}
			}
		}
	}
	if m.dflt != "" && strings.TrimSpace(text) != "" {
		return map[string]string{"mode": m.dflt}
	}
	return nil
}

// keepMode records a mode the user picked that the harness took (a typed
// /mode, a relaunch) as the current one, for harnesses that never report
// it: it stands until a mapper or a screen read says otherwise.
func (s *sess) keepMode(name string, args map[string]any, res harness.ActionResult) {
	m := s.ad.Modes
	if m == nil || name != m.Set {
		return
	}
	switch res.Result {
	case harness.ResultApplied, harness.ResultTyped, harness.ResultRelaunched:
	default:
		return
	}
	id, _ := args["id"].(string)
	if id == "" || !s.ad.modeIDs()[id] {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	under, _ := s.mapperStateRawLocked()
	if s.seed.state == nil {
		s.seed.state, s.seed.under = map[string]string{}, map[string]string{}
	}
	s.seed.state["mode"], s.seed.under["mode"] = id, under["mode"]
	s.refreshStatusLocked()
}
