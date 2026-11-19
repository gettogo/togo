package generic

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// anchorCells are the cells the anchors scenario proves, per agent. The
// scenario runs for every agent whose idle screen is a real capture
// (idleScreens): an agent without one has no pane to anchor against, and
// its cell stays missing until one is captured.
var anchorCells = map[string]func(*testing.T){
	"omp":    func(t *testing.T) { coverage.Covers(t, "generated", "omp/capability/chat.anchors") },
	"claude": func(t *testing.T) { coverage.Covers(t, "generated", "claude/capability/chat.anchors") },
}

// What the engine records of the pane when an item arrives, and what the
// phone gets when it asks where that was: every item of the chat carries
// the pane's position; "Show in terminal" finds it again after the pane
// scrolled, by its line; an item that came later is nearer the bottom; and
// a position whose text is no longer in the pane is not found.
func TestAnchors(t *testing.T) {
	var agents []string
	for a := range idleScreens {
		agents = append(agents, a)
	}
	sort.Strings(agents)
	for _, agent := range agents {
		t.Run(agent, func(t *testing.T) {
			t.Parallel()
			if declare, ok := anchorCells[agent]; ok {
				declare(t)
			} else {
				t.Fatalf("%s has a captured idle screen: claim its chat.anchors cell in anchorCells", agent)
			}
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cwd := filepath.Join(home, "proj")
			if err := os.MkdirAll(cwd, 0o755); err != nil {
				t.Fatal(err)
			}
			// The pane shows the agent's idle screen before the session's
			// records arrive: the engine marks each item where the cursor
			// is when it reads it.
			begun := time.Now().Add(-10 * time.Second).UTC().Truncate(time.Second)
			r := enginetest.New(t, enginetest.Options{Agent: agent, Home: home, Cwd: cwd, ID: enginetest.NewID(),
				Created: begun.Add(-30 * time.Second), NoRedact: true, Anchors: true})
			r.Term.Show(r.Term.MustCapture(idleScreens[agent]))
			l, err := sessiongen.Write(home, sessiongen.For(agent), sessiongen.Script{Seed: 9, Start: begun, Gap: 20 * time.Millisecond, Cwd: cwd,
				Turns: []sessiongen.Turn{{Say: 60, Tools: 1, Output: 40}, {Say: 40}}})
			if err != nil {
				t.Fatal(err)
			}
			if agent == "claude" {
				// Claude Code's transcript is learned from its SessionStart hook.
				raw, _ := json.Marshal(map[string]any{"hook_event_name": "SessionStart", "session_id": strings.TrimSuffix(filepath.Base(l.Main()), ".jsonl"),
					"transcript_path": l.Main(), "source": "startup"})
				r.Deliver("SessionStart", raw)
			}
			st := r.SettleUntil(20*time.Second, enginetest.Has(l.Truth.Last))
			first := anchored(t, st)
			if len(first) < 4 {
				t.Fatalf("%d anchored items, want the prompts, tool steps and replies of two turns", len(first))
			}
			here := *first[0].Anchor
			for _, it := range first {
				if it.Anchor.Line != here.Line || it.Anchor.Needle == "" || it.Anchor.Line < 1 {
					t.Errorf("%s (%s) is anchored at %+v, want the cursor's line %d with text", it.ID, it.Kind, it.Anchor, here.Line)
				}
				loc, err := r.Locate(*it.Anchor)
				if err != nil || !loc.Found || loc.LinesFromBottom != 0 || loc.Needle != it.Anchor.Needle {
					t.Errorf("%s: located %+v %v, want found at the bottom", it.ID, loc, err)
				}
			}
			for _, it := range st.Items {
				if (it.Kind == harness.KindTurn || it.Kind == harness.KindPlan) && it.Anchor != nil {
					t.Errorf("%s (%s) has an anchor: only what the pane showed is marked", it.ID, it.Kind)
				}
			}

			// The agent prints 12 lines: the pane scrolls and the cursor stays.
			var out []string
			for i := 0; i < 12; i++ {
				out = append(out, fmt.Sprintf("output line %d", i))
			}
			r.Term.Output(out...)
			for _, it := range first {
				loc, err := r.Locate(*it.Anchor)
				if err != nil || !loc.Found || loc.LinesFromBottom != 12 {
					t.Errorf("%s: after 12 lines located %+v %v, want found 12 lines up", it.ID, loc, err)
				}
			}
			// A turn that comes later is marked where the cursor is now.
			if err := l.Append(sessiongen.Turn{Say: 30}); err != nil {
				t.Fatal(err)
			}
			st = r.SettleUntil(20*time.Second, enginetest.Has(l.Truth.Last))
			var later []harness.Item
			for _, it := range anchored(t, st) {
				if it.Anchor.Line > here.Line {
					later = append(later, it)
				}
			}
			if len(later) < 2 {
				t.Fatalf("%d items anchored below the first ones, want the last turn's prompt and reply", len(later))
			}
			for _, it := range later {
				if it.Anchor.Line != here.Line+12 {
					t.Errorf("%s anchored at line %d, want %d", it.ID, it.Anchor.Line, here.Line+12)
				}
				if loc, err := r.Locate(*it.Anchor); err != nil || !loc.Found || loc.LinesFromBottom != 0 {
					t.Errorf("%s: located %+v %v", it.ID, loc, err)
				}
			}
			// A position whose text is nowhere in the pane or its history.
			gone := here
			gone.Needle = "text the pane never showed"
			if loc, err := r.Locate(gone); err != nil || loc.Found {
				t.Errorf("a position whose text is not in the pane: located %+v %v, want not found", loc, err)
			}
			if _, err := r.Call("harness.locate", map[string]any{"id": r.ID, "anchor": map[string]any{"line": -1}}); err == nil {
				t.Error("an anchor with a negative line was taken")
			}
		})
	}
}

// anchored are the main-conversation items that carry an anchor, newest
// copy of each; it fails when a prompt, reply or tool step has none.
func anchored(t *testing.T, st enginetest.State) []harness.Item {
	t.Helper()
	var out []harness.Item
	seen := map[string]bool{}
	for i := len(st.Items) - 1; i >= 0; i-- {
		it := st.Items[i]
		if seen[it.ID] || it.Parent != "" || it.Kind == harness.KindTurn || it.Kind == harness.KindPlan || it.Kind == harness.KindNotice {
			continue
		}
		seen[it.ID] = true
		if it.Anchor == nil {
			t.Errorf("%s (%s) has no anchor", it.ID, it.Kind)
			continue
		}
		out = append([]harness.Item{it}, out...)
	}
	return out
}
