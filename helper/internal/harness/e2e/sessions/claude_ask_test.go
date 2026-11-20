package sessions

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	claudehook "github.com/gettogo/togo/helper/internal/adapters/claude"
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
	"github.com/gettogo/togo/helper/internal/wire"
)

const askShots = "harness/adapters/claude/testdata/screens/ask-step"

// hookCall is one Claude Code hook as recorded: its name and raw payload.
type hookCall struct {
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload"`
}

// recordedHooks are the raw payloads of a real Claude Code 2.1.329 run
// (testdata/ask-two-questions-hooks.jsonl).
func recordedHooks(t *testing.T) []hookCall {
	t.Helper()
	f, err := os.Open("../../adapters/claude/testdata/ask-two-questions-hooks.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []hookCall
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var c hookCall
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

// claudeHook does what `togo hook claude <event>` does with a payload:
// reduces it and hands the result to the helper's hook handler.
func claudeHook(t *testing.T, r *enginetest.Rig, event string, payload json.RawMessage) {
	t.Helper()
	ev, _, err := claudehook.Map(event, payload, support.HookEnv(r))
	if err != nil {
		t.Errorf("%s: %v", event, err) // not Fatalf: a hook may run off the test's goroutine
		return
	}
	before := r.Cursor()
	if err := r.Hook(claudehook.Forward(ev, event, payload)); err != nil {
		t.Errorf("local.hook %s: %v", event, err)
		return
	}
	r.WaitCursor(before, 2*time.Second)
}

// askRun is a Claude Code session in which the AskUserQuestion of the
// recorded run is on screen: the transcript has the call, the hooks the
// recording shows came in, and the pane shows the dialog's first screen.
// The pane watcher has not looked yet (r.Look).
type askRun struct {
	r       *enginetest.Rig
	l       *sessiongen.Layout
	id      string
	input   json.RawMessage
	post    json.RawMessage
	tool    string
	stepped []string
	hooks   sync.WaitGroup
}

func startAsk(t *testing.T) *askRun {
	t.Helper()
	var pre, perm, post json.RawMessage
	for _, h := range recordedHooks(t) {
		var p struct {
			ToolName string `json:"tool_name"`
		}
		_ = json.Unmarshal(h.Payload, &p)
		if p.ToolName != "AskUserQuestion" {
			continue
		}
		switch h.Event {
		case "PreToolUse":
			pre = h.Payload
		case "PermissionRequest":
			perm = h.Payload
		case "PostToolUse":
			post = h.Payload
		}
	}
	if pre == nil || perm == nil || post == nil {
		t.Fatal("the recording lacks the AskUserQuestion hooks")
	}
	var p struct {
		ToolUseID string          `json:"tool_use_id"`
		ToolInput json.RawMessage `json:"tool_input"`
	}
	if err := json.Unmarshal(pre, &p); err != nil {
		t.Fatal(err)
	}
	_, l, r := claudeSession(t, time.Hour, nil, enginetest.Options{}, sessiongen.Turn{Say: 10})
	r.SettleUntil(support.FileLimit, enginetest.Has(l.Truth.Last))
	a := &askRun{r: r, l: l, id: p.ToolUseID, input: p.ToolInput, post: post}
	a.write("assistant", map[string]any{"id": "msg_ask", "role": "assistant", "model": "claude-haiku-5-5",
		"content": []any{map[string]any{"type": "tool_use", "id": a.id, "name": "AskUserQuestion", "input": a.input}}}, nil)
	r.SettleUntil(10*time.Second, func(st enginetest.State) bool {
		return len(support.Items(st, support.OfKind(harness.KindQuestion))) == 1
	})
	claudeHook(t, r, "PreToolUse", pre)
	claudeHook(t, r, "PermissionRequest", perm)
	// The dialog as Claude Code draws it: a digit answers the question
	// shown and moves on; the review's Enter submits.
	t.Cleanup(a.hooks.Wait)
	r.Term.OnKey = func(d *enginetest.Dialog, key string, _ bool) bool {
		a.stepped = append(a.stepped, key)
		if d.Screen.Name == askShots+"3.ansi" && (key == "Enter" || key == "1") {
			a.answer(t)
			return true
		}
		return false
	}
	r.Term.Open(&enginetest.Dialog{Screen: r.Term.MustCapture(askShots + "1.ansi"), Next: map[string]enginetest.Screen{
		"1": r.Term.MustCapture(askShots + "2.ansi"), "2": r.Term.MustCapture(askShots + "3.ansi")}})
	return a
}

// write appends a record to Claude Code's transcript.
func (a *askRun) write(kind string, msg map[string]any, result json.RawMessage) {
	rec := map[string]any{"type": kind, "uuid": fmt.Sprintf("ask-%d", time.Now().UnixNano()), "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "message": msg}
	if result != nil {
		rec["toolUseResult"] = result
	}
	b, _ := json.Marshal(rec)
	f, err := os.OpenFile(a.l.Main(), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		a.r.T.Error(err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(b, '\n')); err != nil {
		a.r.T.Error(err)
	}
}

// answer is Claude Code taking the review's submit: the dialog goes, the
// result is written and the PostToolUse hook fires.
func (a *askRun) answer(t *testing.T) {
	var p struct {
		Response json.RawMessage `json:"tool_response"`
	}
	_ = json.Unmarshal(a.post, &p)
	a.r.Term.CloseDialog()
	// Claude Code's hook runs in a process of its own: the key that
	// submitted the dialog does not wait for it.
	a.hooks.Add(1)
	go func() {
		defer a.hooks.Done()
		a.finish(t, p.Response)
	}()
}

func (a *askRun) finish(t *testing.T, response json.RawMessage) {
	a.write("user", map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": a.id, "content": "User has answered your questions"}}}, response)
	claudeHook(t, a.r, "PostToolUse", a.post)
}

// questionOf is the session's one question item.
func questionOf(t *testing.T, st enginetest.State) harness.Item {
	t.Helper()
	qs := support.Items(st, support.OfKind(harness.KindQuestion))
	if len(qs) != 1 {
		t.Fatalf("%d question items, want 1", len(qs))
	}
	return qs[0]
}

// answeredQuestion waits for the question to be settled by its result.
func answeredQuestion(st enginetest.State) bool {
	for _, it := range st.Items {
		if it.Question != nil && len(it.Question.Answered) > 0 {
			return true
		}
	}
	return false
}

// Claude Code's two-question AskUserQuestion (a real 2.1.329 run): the
// chat shows one question item with both questions, their headers, prompts
// and options with the descriptions Claude gave, free text allowed. An
// answer from the phone is sent to the dialog as the keys the dialog
// takes (a digit per question, then the review's Enter), each read back
// from the screen, and the transcript's result settles the item with the
// chosen options.
func TestClaudeTwoQuestionsAnsweredFromThePhone(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "tape", "claude/capability/needs.questions")
	a := startAsk(t)
	r := a.r
	r.Look()
	q := questionOf(t, r.Read()).Question
	if len(q.Questions) != 2 || len(q.Answered) != 0 {
		t.Fatalf("question body %+v", q)
	}
	for i, want := range []struct {
		id, header, prompt string
		labels, details    []string
	}{
		{"q1", "Colour", "Which colour do you prefer?", []string{"Red", "Blue"}, []string{"Choose red", "Choose blue"}},
		{"q2", "Fruit", "Which fruit do you prefer?", []string{"Apple", "Pear"}, []string{"Choose apple", "Choose pear"}},
	} {
		got := q.Questions[i]
		var labels, details []string
		for _, o := range got.Options {
			labels, details = append(labels, o.Label), append(details, o.Detail)
		}
		if got.ID != want.id || got.Header != want.header || got.Prompt != want.prompt || got.Multi || !got.AllowText ||
			!slices.Equal(labels, want.labels) || !slices.Equal(details, want.details) {
			t.Errorf("question %d: %+v, want %+v", i+1, got, want)
		}
	}
	res, err := r.Act(harness.ActionAnswer, a.id, map[string]any{"answers": map[string]any{"q1": []string{"opt1"}, "q2": []string{"opt2"}}})
	if err != nil || res.Result != harness.ResultApplied {
		t.Fatalf("answer: %+v %v", res, err)
	}
	if !slices.Equal(a.stepped, []string{"1", "2", "Enter"}) {
		t.Errorf("the dialog was given %q, want 1 (Red), 2 (Pear), Enter (submit)", a.stepped)
	}
	st := r.SettleUntil(support.FileLimit, answeredQuestion)
	got := questionOf(t, st).Question.Answered
	if !reflect.DeepEqual(got, map[string][]string{"q1": {"opt1"}, "q2": {"opt2"}}) {
		t.Errorf("answered %v, want Red and Pear", got)
	}
	// Answering again is refused: the dialog is gone.
	if res, _ := r.Act(harness.ActionAnswer, a.id, map[string]any{"answers": map[string]any{"q1": []string{"opt1"}, "q2": []string{"opt2"}}}); res.Result != harness.ResultFailed {
		t.Errorf("a second answer gave %+v, want failed", res)
	}
}

// TestKnownFault_ClaudeQuestionDialogShowsAsApprovals: while Claude Code's
// AskUserQuestion dialog is on screen, the phone is offered it twice, and
// neither offer can be answered as an approval. The engine opens an
// approval for the question when the PermissionRequest hook arrives
// (engine/feedapproval.go offerPermission); the pane watcher then reads
// the same dialog as a permission prompt (watch.go, approval/store.go
// PromptSeen), closes the engine's approval as "answered at the desk"
// although nobody answered, and opens a screen approval that is not
// answerable (the detector sees the options but the dialog takes digits
// and a review). Every step of the dialog (colour, fruit, review) does the
// same, so a two-question dialog leaves four approval rows, all resolved at
// the desk, and one pending, not answerable, approval while it is up.
//
// Correct: the question item is the only thing the phone is offered; no
// approval row, and no "desk" resolution nobody made.
//
// Trigger: AskUserQuestion with the dialog on screen and the pane watcher
// looking (any Claude Code 2.1.3xx session at the terminal).
func TestKnownFault_ClaudeQuestionDialogShowsAsApprovals(t *testing.T) {
	t.Parallel()
	a := startAsk(t)
	r := a.r
	offer := r.Pending()
	if len(offer) != 1 || offer[0].Source == wire.SourceScreen {
		t.Fatalf("before the watcher looks: %+v, want the engine's own offer", offer)
	}
	for i, key := range []string{"1", "2", "Enter"} {
		r.Look()
		p := r.WaitPending(1, 3*time.Second)
		if p[0].Source != wire.SourceScreen {
			t.Errorf("step %d: pending %s, want a screen approval", i+1, p[0].Source)
		}
		if i < 2 {
			// The colour and fruit steps list their options and can't be answered.
			if p[0].Answerable == nil || *p[0].Answerable || len(p[0].Options) != 2 || p[0].Options[0].Label != []string{"Red", "Apple"}[i] {
				t.Errorf("step %d: pending %+v, want a not answerable approval offering the question's options", i+1, p[0])
			}
		} else if p[0].Answerable != nil || len(p[0].Options) != 2 || p[0].Options[0].Label != "Submit answers" || p[0].Options[1].Label != "Cancel" {
			// The review is offered as an approval with Submit answers and Cancel.
			t.Errorf("review: pending %+v, want Submit answers and Cancel, answerable", p[0])
		}
		if err := r.Term.Keys(context.Background(), "", key); err != nil {
			t.Fatal(err)
		}
	}
	r.Look()
	st := r.SettleUntil(support.FileLimit, answeredQuestion)
	approvals := support.Items(st, support.OfKind(harness.KindApproval))
	if len(approvals) != 4 {
		t.Fatalf("%d approval rows, want the fault's 4", len(approvals))
	}
	for _, it := range approvals {
		if it.Approval.Resolved != wire.ResolutionDesk || it.Approval.Owner != a.id {
			t.Errorf("approval %+v, want resolved at the desk, owned by the question", it.Approval)
		}
	}
	if p := r.Pending(); len(p) != 0 {
		t.Errorf("%d approvals still pending after the dialog closed", len(p))
	}
}
