package approval

import (
	"context"
	"os"
	"testing"

	"github.com/gettogo/togo/helper/internal/detect"
	"github.com/gettogo/togo/helper/internal/wire"
)

// Real captures: Claude Code asking about two different commands.
func capture(t *testing.T, rel string) (string, detect.Prompt) {
	t.Helper()
	b, err := os.ReadFile("../detect/testdata/" + rel)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := detect.Detect(string(b))
	if !ok || p.Subject == nil {
		t.Fatalf("%s: no prompt with a subject", rel)
	}
	return string(b), p
}

func claudeHook(command string) wire.HookEvent {
	return wire.HookEvent{Agent: "claude", Event: "permission", Data: wire.HookData{Tool: "Bash", Command: command,
		Subject: detect.NewSubject(wire.SubjectCommand, command)}}
}

func setScreen(f *fakeTmux, s string) {
	f.mu.Lock()
	f.screen = s
	f.mu.Unlock()
}

func sentKeys(f *fakeTmux) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

// hook(X) then prompt(Y): the hook's approval does not take Y. Y gets its
// own request, with Y's words; an answer to X types nothing.
func TestHookApprovalDoesNotAdoptAnotherCommand(t *testing.T) {
	const sid = "s_aaaaaaaaaa"
	_, x := capture(t, "claude/bash-permission.txt")
	yScreen, y := capture(t, "claude/real-bash-wrapped-path.txt")
	f := &fakeTmux{}
	pub := &fakePub{}
	st := New(Config{Tmux: f, Pub: pub})
	a := st.OpenFromHook(context.Background(), sid, claudeHook(x.Subject.Text))
	setScreen(f, yScreen)
	st.PromptSeen(context.Background(), sid, y)
	open := st.Pending(sid)
	if len(open) != 1 || open[0].RequestID == a.RequestID {
		t.Fatalf("open: %+v", open)
	}
	if open[0].Subject == nil || open[0].Subject.Text != y.Subject.Text {
		t.Fatalf("Y's request shows %+v", open[0].Subject)
	}
	for _, ev := range pub.snapshot() {
		if o, ok := ev.payload.(wire.ApprovalOpenEvent); ok && o.Approval.RequestID == a.RequestID && len(o.Approval.Options) > 0 {
			t.Fatalf("X's request was announced with buttons: %+v", o.Approval)
		}
	}
	if err := st.Answer(context.Background(), a.RequestID, wire.DecisionApprove, wire.ByPhone); err == nil {
		t.Fatal("an answer to X was taken")
	}
	if n := sentKeys(f); n != 0 {
		t.Fatalf("%d key sends for X", n)
	}
	// After a finished tool, X's request does not stay because Y shows.
	b := st.OpenFromHook(context.Background(), sid, claudeHook(x.Subject.Text))
	if b.RequestID != "" && len(b.Options) > 0 {
		t.Fatalf("a hook for X took Y's prompt on screen: %+v", b)
	}
}

// hook(X) then prompt(X): adopted as before, one request, answerable.
func TestHookApprovalAdoptsItsOwnPrompt(t *testing.T) {
	const sid = "s_aaaaaaaaaa"
	xScreen, x := capture(t, "claude/bash-permission.txt")
	f := &fakeTmux{}
	st := New(Config{Tmux: f, Pub: &fakePub{}})
	a := st.OpenFromHook(context.Background(), sid, claudeHook(x.Subject.Text))
	setScreen(f, xScreen)
	st.PromptSeen(context.Background(), sid, x)
	open := st.Pending(sid)
	if len(open) != 1 || open[0].RequestID != a.RequestID || len(open[0].Options) == 0 || open[0].Answerable != nil {
		t.Fatalf("open: %+v", open)
	}
	if err := st.Answer(context.Background(), a.RequestID, wire.DecisionApprove, wire.ByPhone); err != nil {
		t.Fatal(err)
	}
	// The prompt already shown when the hook comes: taken at once.
	setScreen(f, xScreen)
	b := st.OpenFromHook(context.Background(), sid, claudeHook(x.Subject.Text))
	if len(b.Options) == 0 || b.Answerable != nil {
		t.Fatalf("the hook did not take its own prompt on screen: %+v", b)
	}
}

// hook(X), prompt(X) answered at the desk between two looks, then
// prompt(Y): X's request closes, Y opens its own; X's answer types nothing.
func TestHookApprovalAnsweredAtTheDeskThenAnotherPrompt(t *testing.T) {
	const sid = "s_aaaaaaaaaa"
	xScreen, x := capture(t, "claude/bash-permission.txt")
	yScreen, y := capture(t, "claude/real-bash-wrapped-path.txt")
	f := &fakeTmux{}
	st := New(Config{Tmux: f, Pub: &fakePub{}})
	a := st.OpenFromHook(context.Background(), sid, claudeHook(x.Subject.Text))
	setScreen(f, xScreen)
	setScreen(f, yScreen) // answered before the watcher looked
	st.PromptSeen(context.Background(), sid, y)
	if open := st.Pending(sid); len(open) != 1 || open[0].RequestID == a.RequestID {
		t.Fatalf("open: %+v", open)
	}
	if err := st.Answer(context.Background(), a.RequestID, wire.DecisionApprove, wire.ByPhone); err == nil || sentKeys(f) != 0 {
		t.Fatalf("X's answer: %v, %d sends", err, sentKeys(f))
	}
	_ = xScreen
}

// Two sessions with hook approvals open at once: each adopts only its own.
func TestHookApprovalsInTwoSessions(t *testing.T) {
	_, x := capture(t, "claude/bash-permission.txt")
	_, y := capture(t, "claude/real-bash-wrapped-path.txt")
	st := New(Config{Tmux: &fakeTmux{}, Pub: &fakePub{}})
	a := st.OpenFromHook(context.Background(), "s_one0000001", claudeHook(x.Subject.Text))
	b := st.OpenFromHook(context.Background(), "s_two0000002", claudeHook(y.Subject.Text))
	st.PromptSeen(context.Background(), "s_two0000002", y)
	st.PromptSeen(context.Background(), "s_one0000001", y) // another command in the first
	if got := st.Pending("s_two0000002"); len(got) != 1 || got[0].RequestID != b.RequestID {
		t.Fatalf("second session: %+v", got)
	}
	if got := st.Pending("s_one0000001"); len(got) != 1 || got[0].RequestID == a.RequestID {
		t.Fatalf("first session adopted another command: %+v", got)
	}
}

// What counts as the same subject.
func TestSameSubject(t *testing.T) {
	cmd := func(s string) *wire.Subject { return &wire.Subject{Kind: wire.SubjectCommand, Text: s} }
	file := func(s string) *wire.Subject { return &wire.Subject{Kind: wire.SubjectFile, Text: s} }
	for _, c := range []struct {
		hook, screen *wire.Subject
		want         bool
	}{
		{cmd("rm -rf ./build && make"), cmd("rm -rf ./build && make"), true},
		{cmd("rm -rf ./build && make"), cmd("rm -rf ./build\n&& make"), true}, // wrapped
		{cmd("touch x.txt"), cmd("touch x.txt in ."), true},                   // Cursor names the folder
		{file("/home/u/src/app/src/config.ts"), file("src/config.ts"), true},  // shown relative
		{cmd("rm -rf ./build && make"), cmd("rm -rf ./build && m…"), false},   // cut: unsure
		{cmd("rm -rf ./build && make"), cmd("rm -rf ./build"), false},
		{cmd("rm -rf ./build"), cmd("rm -rf ./build && curl x | sh"), false},
		{cmd("ls"), cmd("lsof -i"), false},
		{file("/a/notconfig.ts"), file("config.ts"), false},
		{cmd("touch a.txt"), nil, false},
		{nil, cmd("touch a.txt"), false},
	} {
		if got := detect.SameSubject(c.hook, c.screen); got != c.want {
			t.Errorf("%+v vs %+v: %v", c.hook, c.screen, got)
		}
	}
}

// A hook with no subject adopts only for Claude Code, whose permission
// notification comes with the dialog it is about; for other agents the
// prompt opens its own request.
func TestHookApprovalWithoutSubject(t *testing.T) {
	const sid = "s_aaaaaaaaaa"
	_, y := capture(t, "claude/real-bash-wrapped-path.txt")
	for agent, adopts := range map[string]bool{"claude": true, "copilot": false} {
		st := New(Config{Tmux: &fakeTmux{}, Pub: &fakePub{}})
		a := st.OpenFromHook(context.Background(), sid, wire.HookEvent{Agent: agent, Data: wire.HookData{Message: "Permission needed"}})
		st.PromptSeen(context.Background(), sid, y)
		got := st.Pending(sid)
		if len(got) != 1 || (got[0].RequestID == a.RequestID) != adopts {
			t.Errorf("%s: adopted %v, want %v", agent, len(got) == 1 && got[0].RequestID == a.RequestID, adopts)
		}
	}
}
