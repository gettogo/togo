package sessions

import (
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// ompIdle is omp's idle screen, a real capture with its input line.
const ompIdle = "harness/engine/testdata/input/omp-idle.txt"

// ompSession writes the turns as an omp session that began age ago and
// starts a rig over it, the pane on omp's idle screen.
func ompSession(t *testing.T, age time.Duration, extra map[int]sessiongen.OMPTurn, o enginetest.Options, turns ...sessiongen.Turn) (support.Env, *sessiongen.Layout, *enginetest.Rig) {
	t.Helper()
	e := support.NewEnv(t, age)
	l, err := sessiongen.Write(e.Home, &sessiongen.OMP{TTY: "ttys042", Extra: extra}, sessiongen.Script{Seed: 51, Start: e.Start, Cwd: e.Cwd, Turns: turns})
	if err != nil {
		t.Fatal(err)
	}
	o.Agent, o.NoRedact = "omp", true
	r := e.RigWith(t, o)
	r.Term.Show(r.Term.MustCapture(ompIdle))
	return e, l, r
}

// everything omp writes that the chat shows: replies, thinking, tool steps
// of each category (read, edit, search, shell), a picture opened by a
// read, a command the user ran with "!", the plan, a question answered, a
// background batch, a job, the notices (model change, compaction, an
// interrupted turn, a failed request) and the turn items.
func ompEverything() (map[int]sessiongen.OMPTurn, []sessiongen.Turn) {
	extra := map[int]sessiongen.OMPTurn{
		1: {Bash: "git status", Think: true, Edits: 2, Search: 1, Picture: "logo.png",
			Todo: []sessiongen.OMPTodo{{Op: "init", Phases: []sessiongen.OMPPhase{{Name: "Fix", Tasks: []string{"Read the test", "Edit the parser"}}, {Name: "Check", Tasks: []string{"Run the tests"}}}}}},
		2: {Model: "moonshot/kimi-k3", Effort: "high",
			Todo: []sessiongen.OMPTodo{{Op: "write", Tasks: []string{"Read the test", "Edit the parser", "Run the tests"}, Done: 2}}},
		5: {Ask: &sessiongen.OMPAsk{ID: "next_focus", Header: "Focus", Question: "What should I work on next?",
			Options: []string{"Bug fixes", "New feature", "Refactoring"}, Answer: "New feature"}},
	}
	turns := []sessiongen.Turn{
		{Say: 200, Tools: 2, Output: 300},
		{Say: 120, Compact: true, Tools: 1, Output: 50},
		{Tools: 1, Output: 40, Interrupt: true},
		{Error: true},
		{Say: 60},
		{Say: 80, Jobs: 1, Agents: &sessiongen.Batch{Size: 2, Steps: 1}},
	}
	return extra, turns
}

// Every kind of item omp's records make reaches the chat, in the order
// omp wrote them, and the history pages fetched from the newest back reach
// the first prompt whatever the page size. The mission shows the plan and
// the status block the records carry.
func TestOMPChatShowsEveryKindInOrder(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "omp/capability/chat", "omp/capability/chat.history", "omp/capability/plan",
		"omp/capability/status")
	extra, turns := ompEverything()
	_, l, r := ompSession(t, time.Hour, extra, enginetest.Options{}, turns...)
	st := r.SettleUntil(support.FileLimit, enginetest.Has(l.Truth.Last))
	support.Check(t, "chat", st, l)

	kinds := map[string]int{}
	for _, it := range st.Items {
		kinds[it.Kind]++
	}
	for _, k := range []string{harness.KindUser, harness.KindAssistant, harness.KindThinking, harness.KindTool, harness.KindTurn,
		harness.KindNotice, harness.KindPlan, harness.KindQuestion, harness.KindAgent, harness.KindBackground} {
		if kinds[k] == 0 {
			t.Errorf("no %s item in the chat: %v", k, kinds)
		}
	}
	if got := len(support.UserPrompts(st)); got != 6 {
		t.Errorf("%d prompts, want 6", got)
	}
	cats := map[string]bool{}
	var picture, bash *harness.ToolBody
	for _, it := range support.Items(st, support.OfKind(harness.KindTool)) {
		cats[it.Tool.Category] = true
		if len(it.Tool.Images) > 0 {
			picture = it.Tool
		}
		if it.Tool.Category == harness.ToolShell && it.Tool.Title == "Run git status" {
			bash = it.Tool
		}
	}
	for _, c := range []string{harness.ToolRead, harness.ToolEdit, harness.ToolSearch, harness.ToolShell, harness.ToolAgent} {
		if !cats[c] {
			t.Errorf("no %s tool step: %v", c, cats)
		}
	}
	if picture == nil || !strings.HasSuffix(picture.Images[0].Path, "/proj/logo.png") || picture.Images[0].MediaType != "image/png" {
		t.Errorf("the picture a read opened: %+v", picture)
	}
	if bash == nil || bash.ExitCode == nil || *bash.ExitCode != 0 || bash.State != harness.StateOK {
		t.Errorf("the command run with !: %+v", bash)
	}
	for code, want := range map[string]int{harness.NoticeModel: 1, harness.NoticeCompacted: 1, harness.NoticeInterrupted: 1} {
		if n := len(support.Items(st, support.Notice(code))); n != want {
			t.Errorf("%d %s notices, want %d", n, code, want)
		}
	}
	var failed *harness.NoticeBody
	for _, it := range support.Items(st, func(it harness.Item) bool { return it.Notice != nil && it.Notice.Level == "error" }) {
		failed = it.Notice
	}
	if failed == nil || failed.Reason != harness.ErrAuth {
		t.Errorf("the failed request's notice: %+v, want an authentication failure", failed)
	}
	states := map[string]int{}
	for _, it := range support.Items(st, func(it harness.Item) bool { return it.Turn != nil }) {
		states[it.Turn.State]++
	}
	if states[harness.StateOK] != 4 || states[harness.StateInterrupted] != 1 || states[harness.StateFailed] != 1 {
		t.Errorf("turns by state %v, want 4 ok, 1 interrupted, 1 failed", states)
	}
	q := support.Items(st, support.OfKind(harness.KindQuestion))
	if len(q) != 1 || strings.Join(q[0].Question.Answered["next_focus"], "+") != "New feature" {
		t.Errorf("the answered question: %+v", q)
	}

	// Items come in the order omp wrote them: by sequence and by time.
	for i := 1; i < len(st.Items); i++ {
		if st.Items[i].Seq <= st.Items[i-1].Seq {
			t.Fatalf("items %d and %d are out of order (seq %d, %d)", i-1, i, st.Items[i-1].Seq, st.Items[i].Seq)
		}
	}
	prompts := support.UserPrompts(st)
	for i := 1; i < len(prompts); i++ {
		if !prompts[i].At.After(prompts[i-1].At) {
			t.Errorf("prompt %d is not after prompt %d", i+1, i)
		}
	}
	if !strings.HasPrefix(prompts[0].User.Text, "Step 1:") || st.More[0] {
		t.Errorf("the first prompt is %q and the oldest page says more is older: %v", prompts[0].User.Text, st.More)
	}

	// The plan: omp's todo tool set it, todo_write moved it on.
	var plan harness.PlanBody
	for _, it := range support.Items(st, support.OfKind(harness.KindPlan)) {
		plan = *it.Plan
	}
	if plan.Total != 3 || plan.Done != 2 || plan.Current != "Run the tests" {
		t.Errorf("plan marker %+v, want 2 of 3 done, running the tests", plan)
	}
	rows := sectionRows(st, harness.SectionPlan)
	if len(rows) != 3 || rows[0].State != harness.RowDone || rows[2].State != harness.RowActive {
		t.Errorf("plan rows %+v", rows)
	}
	if st.Mission.Summary.Plan == nil || st.Mission.Summary.Plan.Done != 2 || st.Mission.Summary.Plan.Total != 3 {
		t.Errorf("summary plan %+v", st.Mission.Summary.Plan)
	}

	// The status block: the model and effort the records changed to.
	sb := support.StatusOf(st)
	if sb.Model != "moonshot/kimi-k3" || sb.Effort != "high" || sb.CostUSD == nil || *sb.CostUSD <= 0 {
		t.Errorf("status %+v, want kimi-k3 at high effort with a cost", sb)
	}
	hasFact := false
	for _, f := range sb.Facts {
		hasFact = hasFact || f.Label == "Last request"
	}
	if !hasFact {
		t.Errorf("status facts %+v, want the last request", sb.Facts)
	}

	for _, size := range []int{1, 7, 25} {
		paged := r.ReadPages(size)
		support.Fail(t, "paging", enginetest.SamePaging(st, paged))
		support.Fail(t, "paging", enginetest.Paging(paged))
		if len(paged.Items) != len(st.Items) || paged.More[0] {
			t.Errorf("pages of %d reach %d of %d items, oldest page more=%v", size, len(paged.Items), len(st.Items), paged.More[0])
		}
	}
	if p := r.ReadPages(7); len(p.Pages) < 3 {
		t.Errorf("%d history pages of 7 items, want several", len(p.Pages))
	}
}

func sectionRows(st enginetest.State, kind string) []harness.Row {
	for _, s := range st.Mission.Sections {
		if s.Kind == kind {
			return s.Rows
		}
	}
	return nil
}
