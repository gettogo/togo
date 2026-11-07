package engine

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// plainAdapter declares no needs.* capability (like omp or aider), no
// answer action, and a status screen.
const plainAdapter = `
adapter: fakeplain
name: Plain
revision: 1
match: { agents: [fakeplain] }
harness: { version: [fakeharness, --version], pattern: '(\d+\.\d+\.\d+)', range: ">=1.0 <2" }
capabilities: [chat, status, agents]
feeds:
  events:
    hook: { events: [Event] }
    mapper: fake.events
    stability: documented
screens:
  usage:
    primitive: status
    title: Usage
    section: status
    capability: status
actions:
  stop:
    label: Stop
    role: destructive
    cli: { argv: [fakeharness, stop, '{{.target}}'] }
    stability: documented
`

func init() { harness.RegisterAdapterFile("fakeplain", []byte(plainAdapter)) }

func plainEngine(t *testing.T, id string, ap Approvals) (*Engine, *sess) {
	t.Helper()
	bin := fakeBin(t, "1.2.3")
	home := t.TempDir()
	pane := &seqPane{mode: "a"}
	e := New(Config{
		Sessions: fakeSessions{id: {ID: id, Agent: "fakeplain", Managed: true, Cwd: home, Command: []string{"fakeplain"}}},
		Typist:   pane, Marker: &fakeMarker{}, Screen: pane.Screen, Home: home, Approvals: ap,
		LoginPath: func(context.Context) string { return bin + ":/bin:/usr/bin" },
		Log:       &Log{NewEpoch: func() string { return "e1" }}, Scale: 0.1,
	})
	t.Cleanup(func() { e.Forget(id) })
	s, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return e, s
}

func logItem(e *Engine, id, item string) (harness.Item, bool) { return e.log.Get(id, item) }

// C3: approvals the screen detector raises show in the chat of an adapter
// that declares no needs capability, and describe says the chat shows them.
func TestApprovalsShowForEveryAdapter(t *testing.T) {
	const id = "s_needsall01"
	ap := &fakeApprovals{pending: []wire.Approval{bashAsk}}
	e, _ := plainEngine(t, id, ap)
	waitFor(t, func() bool { _, ok := logItem(e, id, "approval-rq_bash"); return ok })
	d, err := e.Describe(context.Background(), id)
	if err != nil || !slices.Contains(d.Capabilities, harness.CapNeedsApprovals) {
		t.Fatalf("capabilities %v err %v", d.Capabilities, err)
	}
}

func TestQuestionWithoutAnswerActionIsReadOnly(t *testing.T) {
	const id = "s_needsall02"
	e, _ := plainEngine(t, id, nil)
	deliver(id, map[string]any{"items": []harness.Item{pickColor}})
	waitFor(t, func() bool { _, ok := logItem(e, id, "q1"); return ok })
	it, _ := logItem(e, id, "q1")
	if it.Question.Answerable == nil || *it.Question.Answerable {
		t.Fatalf("answerable = %v", it.Question.Answerable)
	}
}

// H7: the approval that is a question's prompt belongs to the question,
// whichever arrives first.
func TestQuestionOwnsItsApproval(t *testing.T) {
	const id = "s_needsall03"
	ap := &fakeApprovals{}
	e, s := plainEngine(t, id, ap)
	deliver(id, map[string]any{"items": []harness.Item{pickColor}})
	waitFor(t, func() bool { _, ok := logItem(e, id, "q1"); return ok })
	ap.mu.Lock()
	ap.pending = []wire.Approval{colorAsk, bashAsk}
	ap.mu.Unlock()
	e.addApproval(s, colorAsk)
	e.addApproval(s, bashAsk)
	owned, _ := logItem(e, id, "approval-rq_color")
	other, _ := logItem(e, id, "approval-rq_bash")
	q, _ := logItem(e, id, "q1")
	if owned.Approval.Owner != "q1" || other.Approval.Owner != "" || q.Question.RequestID != "rq_color" {
		t.Fatalf("owner %q other %q request %q", owned.Approval.Owner, other.Approval.Owner, q.Question.RequestID)
	}

	// The question arrives after its approval: the approval is relinked.
	const id2 = "s_needsall04"
	ap2 := &fakeApprovals{pending: []wire.Approval{colorAsk}}
	e2, _ := plainEngine(t, id2, ap2)
	waitFor(t, func() bool { _, ok := logItem(e2, id2, "approval-rq_color"); return ok })
	deliver(id2, map[string]any{"items": []harness.Item{pickColor}})
	waitFor(t, func() bool {
		a, _ := logItem(e2, id2, "approval-rq_color")
		return a.Approval != nil && a.Approval.Owner == "q1"
	})
	if q, _ := logItem(e2, id2, "q1"); q.Question.RequestID != "rq_color" {
		t.Fatalf("request %q", q.Question.RequestID)
	}
}

func TestStatusScreenListedOnlyWithAStatusBlock(t *testing.T) {
	const id = "s_needsall05"
	e, _ := plainEngine(t, id, nil)
	listed := func() bool {
		d, _ := e.Describe(context.Background(), id)
		return slices.ContainsFunc(d.Screens, func(r harness.ScreenRef) bool { return r.ID == "usage" })
	}
	if listed() {
		t.Fatal("usage listed with no status block")
	}
	deliver(id, map[string]any{"ops": []harness.Op{{Op: harness.OpStatus, Status: &harness.StatusBlock{Model: "m"}}}})
	waitFor(t, listed)
}

func TestStopNeverRunsForAnUnknownTarget(t *testing.T) {
	const id = "s_needsall06"
	e, _ := plainEngine(t, id, nil)
	res, err := e.Act(context.Background(), harness.ActionRequest{Session: id, Action: harness.ActionStop, Target: "msg_123"})
	if err != nil || res.Result != harness.ResultFailed {
		t.Fatalf("res %+v err %v", res, err)
	}
}

func TestHookOnlyChatSaysItNeedsHooks(t *testing.T) {
	old := hookHintAfter
	hookHintAfter = 30 * time.Millisecond
	t.Cleanup(func() { hookHintAfter = old })
	const id = "s_needsall07"
	e, _ := plainEngine(t, id, nil)
	waitFor(t, func() bool { _, ok := logItem(e, id, hookHintID); return ok })
	if it, _ := logItem(e, id, hookHintID); it.Notice.Level != "warning" {
		t.Fatalf("notice %+v", it.Notice)
	}
	deliver(id, map[string]any{"state": map[string]string{"working": "false"}})
	waitFor(t, func() bool { it, _ := logItem(e, id, hookHintID); return it.Notice != nil && it.Notice.Level == "info" })
}
