package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
	"github.com/gettogo/togo/helper/internal/ports"
)

// claudeIdle is Claude Code's idle screen, a real capture.
const claudeIdle = "harness/engine/testdata/input/claude-suggested-prompt.ansi"

// claudeSession writes the turns as a Claude Code transcript that began
// age ago, starts a rig over it, and gives the engine the transcript's
// path as Claude Code's SessionStart hook does.
func claudeSession(t *testing.T, age time.Duration, extra map[int]sessiongen.ClaudeTurn, o enginetest.Options, turns ...sessiongen.Turn) (support.Env, *sessiongen.Layout, *enginetest.Rig) {
	t.Helper()
	return claudeSessionOn(t, claudeIdle, age, extra, o, turns...)
}

// claudeSessionOn is claudeSession with the pane on another capture from
// the start: the engine reads the mode off the screen as the records
// arrive, so a test that cares which mode it began in must not change the
// screen afterwards.
func claudeSessionOn(t *testing.T, screen string, age time.Duration, extra map[int]sessiongen.ClaudeTurn, o enginetest.Options, turns ...sessiongen.Turn) (support.Env, *sessiongen.Layout, *enginetest.Rig) {
	t.Helper()
	e := support.NewEnv(t, age)
	l, err := sessiongen.Write(e.Home, &sessiongen.Claude{Extra: extra}, sessiongen.Script{Seed: 61, Start: e.Start, Cwd: e.Cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	o.Agent, o.NoRedact = "claude", true
	r := e.RigWith(t, o)
	r.Term.Show(r.Term.MustCapture(screen))
	claudeStart(r, l.Main(), "startup")
	return e, l, r
}

// claudeStart delivers SessionStart for a transcript.
func claudeStart(r *enginetest.Rig, transcript, source string) {
	raw, _ := json.Marshal(map[string]any{"hook_event_name": "SessionStart", "session_id": strings.TrimSuffix(lastPart(transcript), ".jsonl"),
		"transcript_path": transcript, "source": source})
	r.Deliver("SessionStart", raw)
}

// Every kind of item Claude Code's transcript makes reaches the chat in
// order, with the history paged back to the first prompt, and the plan
// from TodoWrite is in the mission.
func TestClaudeChatShowsEveryKindInOrder(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "claude/capability/chat", "claude/capability/chat.history", "claude/capability/plan")
	extra := map[int]sessiongen.ClaudeTurn{1: {Think: true, Edits: 2, Reads: 1, Todo: []string{"Read the test", "Edit the parser", "Run the tests"}, Done: 1}}
	_, l, r := claudeSession(t, time.Hour, extra, enginetest.Options{},
		sessiongen.Turn{Say: 200, Tools: 2, Output: 300},
		sessiongen.Turn{Say: 80, Compact: true, Jobs: 1, Agents: &sessiongen.Batch{Size: 2, Steps: 1, Background: true}},
		sessiongen.Turn{Tools: 1, Output: 40, Interrupt: true},
		sessiongen.Turn{Say: 60})
	st := r.SettleUntil(support.FileLimit, func(st enginetest.State) bool {
		return enginetest.Has(l.Truth.Last)(st) && support.CountKind(st, harness.KindAgent) == 2
	})
	support.Check(t, "chat", st, l)
	kinds := map[string]int{}
	for _, it := range st.Items {
		kinds[it.Kind]++
	}
	for _, k := range []string{harness.KindUser, harness.KindAssistant, harness.KindThinking, harness.KindTool, harness.KindTurn,
		harness.KindNotice, harness.KindPlan, harness.KindAgent, harness.KindBackground} {
		if kinds[k] == 0 {
			t.Errorf("no %s item in the chat: %v", k, kinds)
		}
	}
	if got := len(support.UserPrompts(st)); got != 4 {
		t.Errorf("%d prompts, want 4", got)
	}
	cats := map[string]bool{}
	for _, it := range support.Items(st, support.OfKind(harness.KindTool)) {
		cats[it.Tool.Category] = true
	}
	for _, c := range []string{harness.ToolRead, harness.ToolEdit, harness.ToolShell} {
		if !cats[c] {
			t.Errorf("no %s tool step: %v", c, cats)
		}
	}
	if !hasNotice(st, harness.NoticeCompacted) {
		t.Error("no compaction notice")
	}
	if n := len(support.TurnsIn(st, harness.StateInterrupted)); n != 1 {
		t.Errorf("%d interrupted turns, want 1", n)
	}
	for i := 1; i < len(st.Items); i++ {
		if st.Items[i].Seq <= st.Items[i-1].Seq {
			t.Fatalf("items %d and %d are out of order", i-1, i)
		}
	}
	prompts := support.UserPrompts(st)
	if !strings.HasPrefix(prompts[0].User.Text, "Step 1:") || st.More[0] {
		t.Errorf("the first prompt is %q; oldest page more=%v", prompts[0].User.Text, st.More[0])
	}
	rows := sectionRows(st, harness.SectionPlan)
	if len(rows) != 3 || rows[0].State != harness.RowDone || rows[1].State != harness.RowActive || rows[2].State != harness.RowPending {
		t.Errorf("plan rows %+v", rows)
	}
	if p := st.Mission.Summary.Plan; p == nil || p.Done != 1 || p.Total != 3 {
		t.Errorf("summary plan %+v, want 1 of 3", p)
	}
	for _, size := range []int{1, 7, 25} {
		paged := r.ReadPages(size)
		support.Fail(t, "paging", enginetest.SamePaging(st, paged))
		support.Fail(t, "paging", enginetest.Paging(paged))
		if len(paged.Items) != len(st.Items) || paged.More[0] {
			t.Errorf("pages of %d reach %d of %d items", size, len(paged.Items), len(st.Items))
		}
	}
	if p := r.ReadPages(7); len(p.Pages) < 3 {
		t.Errorf("%d history pages of 7 items, want several", len(p.Pages))
	}
}

// Claude Code's status line input (a real payload) becomes the status
// block: the model, the context window's fill, the cost, and the 5 hour
// limit meter.
func TestClaudeStatusLine(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "claude/capability/status", "claude/capability/status.context", "claude/capability/status.usage")
	_, l, r := claudeSession(t, time.Hour, nil, enginetest.Options{}, sessiongen.Turn{Say: 30})
	r.SettleUntil(support.FileLimit, enginetest.Has(l.Truth.Last))
	raw, err := os.ReadFile("../../adapters/claude/testdata/statusline.json")
	if err != nil {
		t.Fatal(err)
	}
	r.DeliverStatusLine(raw)
	st := r.SettleUntil(support.FileLimit, func(st enginetest.State) bool { return support.StatusOf(st).ContextPercent != nil })
	sb := support.StatusOf(st)
	if sb.Model != "Opus" || sb.ContextPercent == nil || *sb.ContextPercent != 18 || sb.CostUSD == nil || *sb.CostUSD != 0.42 {
		t.Errorf("status %+v, want Opus, 18 percent of the context, $0.42", sb)
	}
	if len(sb.Meters) != 1 || sb.Meters[0].ID != "five_hour" || sb.Meters[0].Percent != 31 || sb.Meters[0].Resets == nil {
		t.Errorf("meters %+v, want the 5 hour limit at 31 percent with its reset", sb.Meters)
	}
}

// The files Claude Code's edits touched are the Changes section's rows,
// and the ports its processes listen on the Ports section's.
func TestClaudeChangesAndPorts(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "claude/capability/changes", "claude/capability/ports")
	e := support.NewEnv(t, time.Hour)
	gitIn(t, e.Cwd, "init", "-q")
	for _, n := range []string{"e0.go", "e1.go"} {
		if err := os.WriteFile(filepath.Join(e.Cwd, n), []byte("package a\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	l, err := sessiongen.Write(e.Home, &sessiongen.Claude{Extra: map[int]sessiongen.ClaudeTurn{1: {Edits: 2}}},
		sessiongen.Script{Seed: 62, Start: e.Start, Cwd: e.Cwd, Turns: []sessiongen.Turn{{Say: 30}}})
	if err != nil {
		t.Fatal(err)
	}
	r := e.RigWith(t, enginetest.Options{Agent: "claude", NoRedact: true, Git: true, Ports: true, Listeners: []ports.Listener{{Port: 8080, Address: "127.0.0.1", PID: 77, Command: "python"}}})
	claudeStart(r, l.Main(), "startup")
	st := r.SettleUntil(support.FileLimit, func(st enginetest.State) bool {
		return enginetest.Has(l.Truth.Last)(st) && len(sectionRows(st, harness.SectionChanges)) == 2 && len(sectionRows(st, harness.SectionPorts)) == 1
	})
	var named []string
	for _, it := range support.Items(st, func(it harness.Item) bool { return it.Tool != nil && it.Tool.Category == harness.ToolEdit }) {
		named = append(named, strings.TrimPrefix(it.Tool.Title, "Edit "))
	}
	rows := sectionRows(st, harness.SectionChanges)
	if strings.Join(named, " ") != "e0.go e1.go" || rows[0].ID != "e0.go" || rows[1].ID != "e1.go" || rows[0].Subtitle != "new" || rows[0].Open == nil || rows[0].Open.Kind != harness.OpenDiff {
		t.Errorf("edit steps (by title) name %v, changes %+v", named, rows)
	}
	if p := sectionRows(st, harness.SectionPorts)[0]; p.ID != "8080" || p.Subtitle != "python" || p.Open == nil || p.Open.Kind != harness.OpenURL {
		t.Errorf("port row %+v", p)
	}
}

func fileSize(t *testing.T, path string) int {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return int(fi.Size())
}
