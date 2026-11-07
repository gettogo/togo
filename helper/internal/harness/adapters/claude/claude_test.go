package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
)

func records(t *testing.T, name string) []harness.Record {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []harness.Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		out = append(out, harness.Record{Source: "transcript", At: time.Unix(0, 0), Data: json.RawMessage(sc.Text())})
	}
	return out
}

type run struct {
	items []harness.Item
	ops   []harness.Op
	final map[string]harness.Item
}

func mapAll(m harness.Mapper, recs []harness.Record) run {
	r := run{final: map[string]harness.Item{}}
	for _, rec := range recs {
		items, ops := m.Map(rec)
		r.items = append(r.items, items...)
		r.ops = append(r.ops, ops...)
		for _, it := range items {
			r.final[it.ID] = it
		}
	}
	return r
}

func env() harness.Env {
	return harness.Env{SessionID: "s_abcdefghij", Cwd: "/HOME/proj", Vars: map[string]string{}}
}

func TestAdapterFileParses(t *testing.T) {
	a, err := spec.Parse(adapterFile)
	if err != nil {
		t.Fatal(err)
	}
	if a.Adapter != "claude" || a.Feeds["transcript"].Mapper != MapperTranscript || a.Screens["plan"].Primitive != "checklist" {
		t.Fatalf("unexpected adapter: %+v", a)
	}
	if !spec.AllowedKeys[a.Actions["set_mode"].Keys.Keys[0]] || !a.Actions["set_mode"].Keys.Cycle {
		t.Fatal("set_mode must cycle an allowed key")
	}
}

func TestTranscriptBasicTurn(t *testing.T) {
	m := newTranscript(env())
	r := mapAll(m, records(t, "transcript-basic.jsonl"))
	var ids []string
	for _, it := range r.items {
		ids = append(ids, it.ID)
	}
	if got := strings.Join(ids, " "); got != "u1 toolu_01 toolu_01 toolu_02 toolu_02 msg_03 u1.turn" {
		t.Fatalf("emission order = %s", got)
	}
	bash := r.final["toolu_01"].Tool
	if bash.Title != "go test ./internal/clock/..." || bash.Category != harness.ToolShell || bash.State != harness.StateFailed ||
		*bash.ExitCode != 1 || bash.Output != "--- FAIL: TestParse (0.00s)\nFAIL" || bash.Subject.Kind != "command" {
		t.Fatalf("bash = %+v", bash)
	}
	edit := r.final["toolu_02"].Tool
	if edit.Title != "Edit internal/clock/parse.go" || edit.Category != harness.ToolEdit || edit.Output != "" ||
		len(edit.Files) != 1 || edit.Files[0] != (harness.FileChange{Path: "internal/clock/parse.go", Added: 3, Removed: 1}) {
		t.Fatalf("edit = %+v", edit)
	}
	turn := r.final["u1.turn"]
	if turn.TurnID != "u1" || turn.Turn.State != harness.StateOK || turn.Turn.Steps != 2 || turn.Turn.FilesChanged != 1 ||
		!turn.Turn.Ended.Equal(time.Date(2026, 11, 3, 12, 1, 16, 0, time.UTC)) {
		t.Fatalf("turn = %+v", turn.Turn)
	}
	if a := r.final["msg_03"]; a.TurnID != "u1" || !strings.HasPrefix(a.Assistant.Text, "The parser") {
		t.Fatalf("reply = %+v", a)
	}
	st := m.State()
	if st["mode"] != "default" || st["model"] != "opus" || st["working"] != "false" || st["turn"] != "u1" {
		t.Fatalf("state = %v", st)
	}
	if len(r.ops) != 1 || r.ops[0].Row.Subtitle != "+3 -1" || r.ops[0].Section != harness.SectionChanges {
		t.Fatalf("ops = %+v", r.ops)
	}
}

func TestTranscriptCases(t *testing.T) {
	m := newTranscript(env())
	r := mapAll(m, records(t, "transcript-cases.jsonl"))
	if u := r.final["c1"]; u.Kind != harness.KindUser || u.User.Text != "/model sonnet" {
		t.Fatalf("command = %+v", u)
	}
	if n := r.final["c2"]; n.Kind != harness.KindNotice || n.Notice.Code != harness.NoticeModel || n.Notice.Text != "Set model to Sonnet" {
		t.Fatalf("command output = %+v", n.Notice)
	}
	if p := r.final["plan-u2"]; p.Kind != harness.KindPlan || p.Plan.Done != 1 || p.Plan.Total != 2 || p.Plan.Current != "Write the notes" {
		t.Fatalf("plan = %+v", p.Plan)
	}
	if _, ok := r.final["toolu_10"]; ok {
		t.Fatal("plan tools must not show as steps")
	}
	q := r.final["toolu_11"]
	if q.Kind != harness.KindQuestion || q.Question.Questions[0].Prompt != "Which channel?" || q.Question.Questions[0].Options[1].ID != "opt2" {
		t.Fatalf("question = %+v", q.Question)
	}
	ag := r.final["agent-ag1"]
	if ag.Kind != harness.KindAgent || ag.Agent.Name != "Check the changelog" || ag.Agent.Role != "Explore" || ag.Agent.State != harness.StateOK || ag.TurnID != "u2" {
		t.Fatalf("agent = %+v %+v", ag, ag.Agent)
	}
	for _, id := range []string{"sc1", "toolu_s1", "msg_s2"} {
		if it := r.final[id]; it.Parent != "agent-ag1" || it.TurnID != "" {
			t.Fatalf("sidechain item %s = parent %q turn %q", id, it.Parent, it.TurnID)
		}
	}
	bg := r.final["bg-bg7"]
	if bg.Kind != harness.KindBackground || bg.Background.State != harness.StateFailed || bg.Background.Command != "npm run dev" {
		t.Fatalf("background = %+v", bg.Background)
	}
	if turn := r.final["u2.turn"]; turn.Turn.State != harness.StateInterrupted || turn.Turn.Steps != 2 {
		t.Fatalf("interrupted turn = %+v", turn.Turn)
	}
	if n := r.final["i1"]; n.Notice == nil || n.Notice.Code != harness.NoticeInterrupted {
		t.Fatalf("interrupt notice = %+v", n)
	}
	if n := r.final["cb1"]; n.Notice == nil || n.Notice.Code != harness.NoticeCompacted {
		t.Fatalf("compaction = %+v", n)
	}
	if _, ok := r.final["cs1"]; ok {
		t.Fatal("the compaction summary is not a prompt")
	}
	var sections = map[string]int{}
	for _, op := range r.ops {
		sections[op.Section]++
	}
	if sections[harness.SectionPlan] != 1 || sections[harness.SectionAgents] < 1 || sections[harness.SectionBackground] != 2 {
		t.Fatalf("ops by section = %v", sections)
	}
	if m.State()["mode"] != "plan" || m.State()["model"] != "sonnet" {
		t.Fatalf("state = %v", m.State())
	}
}

// TestTranscriptRealRecords maps scrubbed lines copied from real Claude
// Code 2.1.3xx transcripts: every tool call completes and nothing panics.
func TestTranscriptRealRecords(t *testing.T) {
	m := newTranscript(env())
	r := mapAll(m, append(records(t, "transcript-real.jsonl"), records(t, "subagent-real.jsonl")...))
	kinds := map[string]int{}
	for _, it := range r.final {
		kinds[it.Kind]++
		if it.Kind == harness.KindTool && it.Tool.State == harness.StateRunning && it.Tool.Name != "Agent" {
			t.Errorf("tool %s (%s) never completed", it.ID, it.Tool.Name)
		}
	}
	for _, k := range []string{harness.KindUser, harness.KindTool, harness.KindTurn, harness.KindAgent, harness.KindBackground, harness.KindNotice, harness.KindPlan} {
		if kinds[k] == 0 {
			t.Errorf("no %s items from real records: %v", k, kinds)
		}
	}
	sub := 0
	for _, it := range r.final {
		if it.Parent != "" {
			sub++
		}
	}
	if sub == 0 {
		t.Error("subagent records found no parent")
	}
}

func TestHooksMapper(t *testing.T) {
	h := newHooks(env())
	var items []harness.Item
	var ops []harness.Op
	for _, rec := range records(t, "hooks.jsonl") {
		i, o := h.Map(rec)
		items, ops = append(items, i...), append(ops, o...)
	}
	st := h.State()
	if st["harness_session"] != "0b6e5f8e-1111-4222-8333-944455556666" || st["mode"] != "acceptEdits" || st["working"] != "false" ||
		!strings.HasSuffix(st["transcript_dir"], "0b6e5f8e-1111-4222-8333-944455556666") || st["model"] != "opus" {
		t.Fatalf("state = %v", st)
	}
	if len(ops) != 2 || ops[1].Row.State != harness.RowDone || ops[0].Row.Title != "Find the failing test" {
		t.Fatalf("ops = %+v", ops)
	}
	if len(items) != 1 || items[0].Notice.Code != harness.NoticeRateLimit {
		t.Fatalf("items = %+v", items)
	}
}

func TestStatuslineMapper(t *testing.T) {
	b, _ := os.ReadFile(filepath.Join("testdata", "statusline.json"))
	s := newStatusline(env())
	_, ops := s.Map(harness.Record{Data: b})
	if len(ops) != 1 || ops[0].Op != harness.OpStatus {
		t.Fatalf("ops = %+v", ops)
	}
	st := ops[0].Status
	if st.Model != "Opus" || *st.ContextPercent != 18 || *st.CostUSD != 0.42 || len(st.Meters) != 1 ||
		st.Meters[0].Label != "5 hour limit" || !st.Meters[0].Resets.Equal(time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC)) {
		t.Fatalf("status = %+v", st)
	}
	if s.State()["model"] != "opus" {
		t.Fatalf("state = %v", s.State())
	}
	if _, again := s.Map(harness.Record{Data: b}); again != nil {
		t.Fatal("an unchanged status line must not emit")
	}
}

func TestAliasAndReadMode(t *testing.T) {
	for in, want := range map[string]string{"claude-opus-5-5": "opus", "claude-sonnet-4-6[1m]": "sonnet[1m]",
		"Opus 4.6 (1M context)": "opus[1m]", "claude-haiku-4-5": "haiku", "gpt-5": "gpt-5"} {
		if got := Alias(in); got != want {
			t.Errorf("Alias(%q) = %q, want %q", in, got, want)
		}
	}
	screens := map[string]string{
		"output\n╭────╮\n│ >  │\n╰────╯\n  ⏸ plan mode on (shift+tab to cycle)\n": "plan",
		"│ > │\n  ⏵⏵ accept edits on (shift+tab to cycle)":                        "acceptEdits",
		"│ > │\n  ⏵⏵ bypass permissions on (shift+tab to cycle)":                  "bypassPermissions",
		"text\n╭──╮\n│ > │\n╰──╯\n  ? for shortcuts\n":                            "default",
		"│ ❯ \n  ? for shortcuts":                                                 "default",
	}
	for text, want := range screens {
		if got := ReadMode(text)["mode"]; got != want {
			t.Errorf("ReadMode(%q) = %q, want %q", text, got, want)
		}
	}
	if ReadMode("Do you want to proceed?\n❯ 1. Yes\n  2. No") != nil {
		t.Error("a dialog is not the prompt box")
	}
	if ReadMode("Compiling...\n") != nil {
		t.Error("a screen without the prompt box has no mode")
	}
}
