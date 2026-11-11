package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/spec"
	"github.com/gettogo/togo/helper/internal/wire"
)

// askingApprovals is fakeApprovals with the store's ask approvals: what was
// offered, the channel each answer arrives on, and what was closed.
type askingApprovals struct {
	fakeApprovals
	asked  []wire.AskParams
	ids    []string
	chans  []chan string
	closed []string
}

func (f *askingApprovals) OpenAsk(_ context.Context, p wire.AskParams) (wire.Approval, <-chan string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := "rq_ask" + string(rune('1'+len(f.asked)))
	ch := make(chan string, 1)
	f.asked, f.ids, f.chans = append(f.asked, p), append(f.ids, id), append(f.chans, ch)
	a := wire.Approval{RequestID: id, SessionID: p.SessionID, Source: wire.SourceAsk, Title: p.Title, CreatedAt: time.Unix(5, 0)}
	for i, label := range p.Options {
		a.Options = append(a.Options, wire.Option{N: i + 1, Label: label})
	}
	f.pending = append(f.pending, a)
	return a, ch
}

func (f *askingApprovals) CloseDesk(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = append(f.closed, id)
}

func (f *askingApprovals) snapshot() (asked []wire.AskParams, ids, closed []string, chans []chan string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]wire.AskParams(nil), f.asked...), append([]string(nil), f.ids...),
		append([]string(nil), f.closed...), append([]chan string(nil), f.chans...)
}

// permission is a question as the OpenCode adapter sends a permission.
func permission(prompt string, answered map[string][]string) harness.Item {
	return harness.Item{ID: "per_1~ses_1", Kind: harness.KindQuestion, Question: &harness.QuestionBody{
		Questions: []harness.Question{{ID: "0", Header: "Permission", Prompt: prompt, Options: []harness.Choice{
			{ID: "once", Label: "Allow once"}, {ID: "always", Label: "Always allow"}, {ID: "reject", Label: "Deny"},
		}}},
		Answered: answered, Approve: "once", Deny: "reject",
	}}
}

// answering gives the session's adapter an answer action, as OpenCode's has.
func answering(s *sess) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ad.Actions == nil {
		s.ad.Actions = map[string]spec.Action{}
	}
	s.ad.Actions[harness.ActionAnswer] = spec.Action{}
}

type actCalls struct {
	mu    sync.Mutex
	calls []harness.ActionRequest
}

func (a *actCalls) act(_ context.Context, req harness.ActionRequest) (harness.ActionResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, req)
	return harness.ActionResult{Result: harness.ResultApplied}, nil
}

func (a *actCalls) list() []harness.ActionRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]harness.ActionRequest(nil), a.calls...)
}

// A permission the adapter answers through its API is offered as an
// approval once, Approve and Deny only, titled with its prompt; Approve from
// the phone answers the question "once" through the adapter.
func TestPermissionIsOfferedAsAnApprovalAndApproveAnswersOnce(t *testing.T) {
	const id = "s_perm01"
	ap := &askingApprovals{}
	e, s := questionEngine(t, id, &ap.fakeApprovals)
	e.cfg.Approvals = ap
	answering(s)
	calls := &actCalls{}
	e.act = calls.act

	s.mu.Lock()
	s.publishLocked([]harness.Item{permission("Allow shell: npm test", nil)}, nil)
	s.publishLocked([]harness.Item{permission("Allow shell: npm test", nil)}, nil) // an update offers nothing new
	s.mu.Unlock()
	waitFor(t, func() bool { _, ids, _, _ := ap.snapshot(); return len(ids) == 1 })

	asked, ids, _, chans := ap.snapshot()
	p := asked[0]
	if p.Title != "Allow shell: npm test" || strings.Join(p.Options, "|") != "Allow once|Deny" ||
		p.Approve != "Allow once" || p.Deny != "Deny" || p.SessionID == nil || *p.SessionID != id {
		t.Fatalf("offered %+v", p)
	}
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.needs.asked["per_1~ses_1"] == ids[0]
	})
	chans[0] <- "Allow once"
	waitFor(t, func() bool { return len(calls.list()) == 1 })
	req := calls.list()[0]
	var answers map[string][]string
	if err := json.Unmarshal(req.Args["answers"], &answers); err != nil {
		t.Fatal(err)
	}
	if req.Session != id || req.Action != harness.ActionAnswer || req.Target != "per_1~ses_1" || strings.Join(answers["0"], ",") != "once" {
		t.Fatalf("answered %+v %v", req, answers)
	}
}

// Deny from the phone rejects the permission.
func TestPermissionDenyFromThePhoneRejects(t *testing.T) {
	const id = "s_perm02"
	ap := &askingApprovals{}
	e, s := questionEngine(t, id, &ap.fakeApprovals)
	e.cfg.Approvals = ap
	answering(s)
	calls := &actCalls{}
	e.act = calls.act
	s.mu.Lock()
	s.publishLocked([]harness.Item{permission("Allow edit: src/app.ts", nil)}, nil)
	s.mu.Unlock()
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.needs.asked["per_1~ses_1"] != ""
	})
	_, _, _, chans := ap.snapshot()
	chans[0] <- "Deny"
	waitFor(t, func() bool { return len(calls.list()) == 1 })
	if got := string(calls.list()[0].Args["answers"]); got != `{"0":["reject"]}` {
		t.Fatalf("answers %s", got)
	}
}

// Answered in the app or at the terminal, the offered approval closes, so the
// Lock Screen and the Watch let go of it; nothing is answered twice.
func TestPermissionAnsweredElsewhereClosesItsApproval(t *testing.T) {
	const id = "s_perm03"
	ap := &askingApprovals{}
	e, s := questionEngine(t, id, &ap.fakeApprovals)
	e.cfg.Approvals = ap
	answering(s)
	calls := &actCalls{}
	e.act = calls.act
	s.mu.Lock()
	s.publishLocked([]harness.Item{permission("Allow shell: npm test", nil)}, nil)
	s.mu.Unlock()
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.needs.asked["per_1~ses_1"] != ""
	})
	s.mu.Lock()
	s.publishLocked([]harness.Item{permission("Allow shell: npm test", map[string][]string{"0": {"always"}})}, nil)
	s.mu.Unlock()
	_, ids, _, _ := ap.snapshot()
	waitFor(t, func() bool { _, _, closed, _ := ap.snapshot(); return len(closed) == 1 && closed[0] == ids[0] })
	if len(calls.list()) != 0 {
		t.Fatalf("answered again: %+v", calls.list())
	}
}

// Every open question is offered: a plain single-choice question with its
// options (a label picked from a notification answers it), a question the
// phone cannot answer with nothing to pick (the push still says the agent
// waits), an answered one never, and nothing without ask approvals.
func TestEveryOpenQuestionIsOffered(t *testing.T) {
	const id = "s_perm04"
	ap := &askingApprovals{}
	e, s := questionEngine(t, id, &ap.fakeApprovals)
	e.cfg.Approvals = ap
	answering(s)
	calls := &actCalls{}
	e.act = calls.act
	answered := permission("Allow shell: ls", map[string][]string{"0": {"once"}})
	s.mu.Lock()
	s.publishLocked([]harness.Item{pickColor, answered}, nil)
	s.mu.Unlock()
	waitFor(t, func() bool { _, ids, _, _ := ap.snapshot(); return len(ids) == 1 })
	asked, _, _, chans := ap.snapshot()
	if p := asked[0]; p.Title != "Which color?" || strings.Join(p.Options, "|") != "Red|Blue" || p.Approve != "" || p.Deny != "" {
		t.Fatalf("offered %+v", p)
	}
	chans[0] <- "Blue"
	waitFor(t, func() bool { return len(calls.list()) == 1 })
	if req := calls.list()[0]; req.Target != "q1" || string(req.Args["answers"]) != `{"color":["c2"]}` {
		t.Fatalf("answered %+v %s", req, req.Args["answers"])
	}

	e3, s3 := questionEngine(t, "s_perm07", &fakeApprovals{})
	asking := &askingApprovals{}
	e3.cfg.Approvals = asking
	s3.mu.Lock()
	delete(s3.ad.Actions, harness.ActionAnswer)
	s3.publishLocked([]harness.Item{permission("Allow shell: ls", nil)}, nil)
	s3.mu.Unlock()
	waitFor(t, func() bool { _, ids, _, _ := asking.snapshot(); return len(ids) == 1 })
	if a, _, _, _ := asking.snapshot(); len(a[0].Options) != 0 || a[0].Title != "Allow shell: ls" {
		t.Fatalf("a question the phone cannot answer offered choices: %+v", a[0])
	}

	e2, s2 := questionEngine(t, "s_perm05", &fakeApprovals{})
	_ = e2
	s2.mu.Lock()
	s2.publishLocked([]harness.Item{permission("Allow shell: ls", nil)}, nil)
	defer s2.mu.Unlock()
	if len(s2.needs.asked) != 0 {
		t.Fatalf("offered without ask approvals: %v", s2.needs.asked)
	}
}

// A batch, a multi-select and a long list are offered with nothing to pick:
// the Lock Screen says to open Togo, where the card answers them.
func TestBatchesAndMultiSelectsAreOfferedWithoutChoices(t *testing.T) {
	const id = "s_perm08"
	ap := &askingApprovals{}
	e, s := questionEngine(t, id, &ap.fakeApprovals)
	e.cfg.Approvals = ap
	answering(s)
	multi := pickColor
	multi.ID, multi.Question = "q-multi", &harness.QuestionBody{Questions: []harness.Question{{ID: "0", Prompt: "Which colors?", Multi: true,
		Options: []harness.Choice{{ID: "a", Label: "Red"}, {ID: "b", Label: "Blue"}}}}}
	batch := pickColor
	batch.ID, batch.Question = "q-batch", &harness.QuestionBody{Questions: []harness.Question{
		{ID: "0", Prompt: "Which color?", Options: []harness.Choice{{ID: "a", Label: "Red"}}},
		{ID: "1", Header: "Size", Prompt: "Which size?", Options: []harness.Choice{{ID: "s", Label: "Small"}}},
	}}
	s.mu.Lock()
	s.publishLocked([]harness.Item{multi, batch}, nil)
	s.mu.Unlock()
	waitFor(t, func() bool { _, ids, _, _ := ap.snapshot(); return len(ids) == 2 })
	asked, _, _, _ := ap.snapshot()
	for _, p := range asked {
		if len(p.Options) != 0 {
			t.Fatalf("offered choices for %q: %v", p.Title, p.Options)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	needs := s.mission.section(harness.SectionNeeds)
	if needs == nil || len(needs.Rows) != 2 {
		t.Fatalf("needs rows %+v", needs)
	}
	for _, r := range needs.Rows {
		if r.ID == "q-batch" && r.Subtitle != "2 questions" {
			t.Fatalf("batch row %+v", r)
		}
	}
}

// An open question is a Needs row of its own, so the roster and the
// summary say needs-you for an adapter that emits no row and declares no
// needs capability; the answer takes the row away.
func TestOpenQuestionIsANeedsRowUntilAnswered(t *testing.T) {
	const id = "s_perm09"
	e, s := questionEngine(t, id, &fakeApprovals{})
	_ = e
	s.mu.Lock()
	s.publishLocked([]harness.Item{pickColor}, nil)
	needs := s.mission.section(harness.SectionNeeds)
	if needs == nil || len(needs.Rows) != 1 || needs.Rows[0].ID != "q1" || needs.Rows[0].Title != "Which color?" ||
		needs.Rows[0].State != harness.StateWaiting || s.mission.summary.NeedsYou != 1 {
		t.Fatalf("needs %+v summary %+v", needs, s.mission.summary)
	}
	done := pickColor
	done.Question = &harness.QuestionBody{Questions: pickColor.Question.Questions, Answered: map[string][]string{"color": {"c1"}}}
	s.publishLocked([]harness.Item{done}, nil)
	needs = s.mission.section(harness.SectionNeeds)
	s.mu.Unlock()
	if len(needs.Rows) != 0 || s.mission.summary.NeedsYou != 0 {
		t.Fatalf("after the answer: %+v", needs)
	}
}

// A prompt the watcher finds on screen for an offered question takes it
// over: the offered ask closes, the question links to the screen request,
// and the question's row stays the only one.
func TestScreenPromptTakesOverAnOfferedQuestion(t *testing.T) {
	const id = "s_perm10"
	ap := &askingApprovals{}
	e, s := questionEngine(t, id, &ap.fakeApprovals)
	e.cfg.Approvals = ap
	answering(s)
	s.mu.Lock()
	s.publishLocked([]harness.Item{pickColor}, nil)
	s.mu.Unlock()
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.needs.asked["q1"] != ""
	})
	_, ids, _, _ := ap.snapshot()
	screen := wire.Approval{RequestID: "rq_screen", SessionID: ptrStr(id), Source: wire.SourceScreen, Title: "Which color?", CreatedAt: time.Unix(3, 0),
		Options: []wire.Option{{N: 1, Label: "Red"}, {N: 2, Label: "Blue"}}}
	ap.mu.Lock()
	ap.pending = append(ap.pending, screen)
	ap.mu.Unlock()
	e.addApproval(s, screen)
	waitFor(t, func() bool { _, _, closed, _ := ap.snapshot(); return len(closed) == 1 && closed[0] == ids[0] })
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.needs.request["q1"] != "rq_screen" {
		t.Fatalf("linked to %q", s.needs.request["q1"])
	}
	if _, offered := s.needs.asked["q1"]; offered {
		t.Fatal("still offered")
	}
	it, _ := e.log.Get(id, "q1")
	if it.Question.RequestID != "rq_screen" {
		t.Fatalf("question request %q", it.Question.RequestID)
	}
	rows := s.mission.section(harness.SectionNeeds).Rows
	if len(rows) != 1 || rows[0].ID != "q1" {
		t.Fatalf("rows %+v", rows)
	}
}

// An offered ask that expires while the agent still waits is offered
// again, so the session never drops out of needs-you.
func TestExpiredOfferIsRenewed(t *testing.T) {
	const id = "s_perm11"
	ap := &askingApprovals{}
	e, s := questionEngine(t, id, &ap.fakeApprovals)
	e.cfg.Approvals = ap
	answering(s)
	s.mu.Lock()
	s.publishLocked([]harness.Item{pickColor}, nil)
	s.mu.Unlock()
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.needs.asked["q1"] != ""
	})
	_, ids, _, _ := ap.snapshot()
	ap.mu.Lock()
	first := ap.pending[0]
	ap.mu.Unlock()
	e.addApproval(s, first)
	ap.mu.Lock()
	ap.pending = nil
	ap.mu.Unlock()
	e.approvalClosed(wire.Event{Payload: wire.ApprovalClosedEvent{RequestID: ids[0], Resolution: wire.ResolutionExpired}})
	waitFor(t, func() bool { _, ids, _, _ := ap.snapshot(); return len(ids) == 2 })
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.needs.asked["q1"] == ids[0] || s.needs.request["q1"] == ids[0] {
		t.Fatalf("still on the expired ask: %v %v", s.needs.asked, s.needs.request)
	}
}

func ptrStr(s string) *string { return &s }

// The offered approval belongs to its question, even with a prompt longer
// than an approval title, and adds no second Needs row.
func TestOfferedApprovalIsItsQuestionsNotASecondNeed(t *testing.T) {
	const id = "s_perm06"
	ap := &askingApprovals{}
	e, s := questionEngine(t, id, &ap.fakeApprovals)
	e.cfg.Approvals = ap
	answering(s)
	long := "Allow shell: " + strings.Repeat("npm run test:unit -- --reporter dot ", 12)
	s.mu.Lock()
	s.publishLocked([]harness.Item{permission(long, nil)}, nil)
	s.mu.Unlock()
	waitFor(t, func() bool { _, ids, _, _ := ap.snapshot(); return len(ids) == 1 })
	ap.mu.Lock()
	a := ap.pending[0]
	ap.mu.Unlock()
	a.Title = string([]rune(long)[:wire.MaxTitle])

	e.addApproval(s, a)
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := e.log.Get(id, "approval-"+a.RequestID)
	if !ok || it.Approval == nil || it.Approval.Owner != "per_1~ses_1" {
		t.Fatalf("approval item %+v %v", it, ok)
	}
	if needs := s.mission.section(harness.SectionNeeds); needs != nil {
		for _, r := range needs.Rows {
			if r.ID == a.RequestID {
				t.Fatalf("a second Needs row: %+v", r)
			}
		}
	}
}

// A question the phone cannot answer is offered with nothing to pick, but
// never beside a prompt the watcher found: the screen prompt is the real
// request, so the empty offer closes when one opens, is not opened while
// one is pending, and comes back once the screen prompt is gone.
func TestEmptyOfferYieldsToAScreenPrompt(t *testing.T) {
	const id = "s_perm12"
	ap := &askingApprovals{}
	e, s := questionEngine(t, id, &ap.fakeApprovals)
	e.cfg.Approvals = ap
	s.mu.Lock()
	delete(s.ad.Actions, harness.ActionAnswer)
	s.publishLocked([]harness.Item{pickColor}, nil)
	s.mu.Unlock()
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.needs.asked["q1"] != ""
	})
	_, ids, _, _ := ap.snapshot()
	screen := wire.Approval{RequestID: "rq_goose", SessionID: ptrStr(id), Source: wire.SourceScreen, Title: "Allow this tool?", CreatedAt: time.Unix(3, 0),
		Options: []wire.Option{{N: 1, Label: "Allow once"}, {N: 2, Label: "Deny"}}}
	ap.mu.Lock()
	ap.pending = append(ap.pending, screen)
	ap.mu.Unlock()
	e.addApproval(s, screen)
	waitFor(t, func() bool { _, _, closed, _ := ap.snapshot(); return len(closed) == 1 && closed[0] == ids[0] })
	s.mu.Lock()
	rows := s.mission.section(harness.SectionNeeds).Rows
	s.mu.Unlock()
	if len(rows) != 2 {
		t.Fatalf("rows %+v", rows) // the question's and the screen prompt's
	}
	time.Sleep(30 * time.Millisecond)
	if _, ids, _, _ := ap.snapshot(); len(ids) != 1 {
		t.Fatalf("offered again beside the screen prompt: %v", ids)
	}
	ap.mu.Lock()
	ap.pending = nil
	ap.mu.Unlock()
	e.approvalClosed(wire.Event{Payload: wire.ApprovalClosedEvent{RequestID: "rq_goose", Resolution: wire.ResolutionDesk}})
	waitFor(t, func() bool { _, ids, _, _ := ap.snapshot(); return len(ids) == 2 })
}

// Only a permission, or a question whose two options are a yes and a no,
// is offered with Approve and Deny. A question with more choices, or two
// that are not a yes and a no, is offered plain: its words, no roles, so
// no surface draws a Yes that would pick one of them.
func TestOnlyBinaryQuestionsAreOfferedAsApproveDeny(t *testing.T) {
	q := func(labels ...string) harness.Item {
		var opts []harness.Choice
		for _, l := range labels {
			opts = append(opts, harness.Choice{ID: l, Label: l})
		}
		return harness.Item{ID: "q", Kind: harness.KindQuestion, Question: &harness.QuestionBody{Questions: []harness.Question{{ID: "0", Prompt: "What next?", Options: opts}}}}
	}
	perm := permission("Allow shell: ls", nil)
	if o := askOffer(perm); o.plain || o.approve != "Allow once" || o.deny != "Deny" || len(o.options) != 2 {
		t.Fatalf("permission %+v", o)
	}
	if o := askOffer(q("Yes", "No")); o.plain || o.approve != "Yes" || o.deny != "No" {
		t.Fatalf("yes/no %+v", o)
	}
	if o := askOffer(q("No", "Yes")); o.plain || o.approve != "Yes" || o.deny != "No" {
		t.Fatalf("no/yes %+v", o)
	}
	for _, it := range []harness.Item{
		q("Test app input", "Make code changes", "Run a workflow", "Something else"),
		q("Yes", "No", "Maybe"),
		q("Red", "Blue"),
		q("Allow", "Always allow"),
	} {
		o := askOffer(it)
		if !o.plain || o.approve != "" || o.deny != "" || len(o.options) != len(it.Question.Questions[0].Options) {
			t.Fatalf("%v: %+v", it.Question.Questions[0].Options, o)
		}
	}
}
