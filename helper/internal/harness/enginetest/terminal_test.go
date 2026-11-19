package enginetest

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

func rigFor(t *testing.T, agent string) *Rig {
	t.Helper()
	home, _ := filepath.EvalSymlinks(t.TempDir())
	return New(t, Options{Agent: agent, Home: home, Cwd: filepath.Join(home, "proj"),
		Created: time.Now().Add(-time.Minute), ID: NewID()})
}

// The input line is found in real captures by the adapter's own pattern:
// omp's empty line, omp's line holding leftover text, and Claude Code's
// line with a dim suggestion, which is not typed text.
func TestTerminalReadsCapturedInputLines(t *testing.T) {
	omp := newTerminal("omp")
	idle := omp.MustCapture("harness/engine/testdata/input/omp-idle.txt")
	if idle.Input < 0 || idle.Text != "" || idle.Prefix != "╰─ " {
		t.Fatalf("omp idle: row %d prefix %q text %q", idle.Input, idle.Prefix, idle.Text)
	}
	left := omp.MustCapture("harness/engine/testdata/input/omp-leftover.txt")
	if left.Text != "/login anthropic" {
		t.Fatalf("omp leftover: text %q", left.Text)
	}
	cc := newTerminal("claude")
	sug := cc.MustCapture("harness/engine/testdata/input/claude-suggested-prompt.ansi")
	if sug.Input < 0 || sug.Text != "" {
		t.Fatalf("claude suggestion: row %d text %q", sug.Input, sug.Text)
	}
	typed := cc.MustCapture("harness/engine/testdata/input/claude-typed-over-suggestion.ansi")
	if typed.Text != "x" {
		t.Fatalf("claude typed: text %q", typed.Text)
	}
	cc.Show(sug)
	_ = cc.Type(t.Context(), "", "hello there", false, false)
	if got := cc.Line(); got != "hello there" {
		t.Fatalf("line %q", got)
	}
}

// A prompt sent while idle goes through the engine's checked typing: the
// leftover text in omp's line is cleared first, the prompt is pasted, Enter
// takes it out of the line, and the agent is given exactly the prompt.
func TestRigSendClearsLeftoverAndSubmits(t *testing.T) {
	r := rigFor(t, "omp")
	r.Term.Show(r.Term.MustCapture("harness/engine/testdata/input/omp-leftover.txt"))
	var got []string
	r.Term.OnSubmit = func(text string) { got = append(got, text) }
	res, err := r.Send("Fix the parser", harness.SendNow)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.Item.User == nil || res.Item.User.Queued {
		t.Fatalf("send result %+v", res.Item)
	}
	if !slices.Equal(got, []string{"Fix the parser"}) {
		t.Fatalf("agent was given %q", got)
	}
	if keys := r.Term.KeysSent(); !slices.Contains(keys, "C-u") {
		t.Errorf("leftover not cleared with C-u: keys %v", keys)
	}
	if r.Term.Line() != "" {
		t.Errorf("input line still holds %q", r.Term.Line())
	}
}

// A screen dialog seen by the watcher reaches Needs you as an approval;
// answered from the phone, its keys go to the pane and it closes.
func TestRigApprovalFromScreenAnsweredFromPhone(t *testing.T) {
	r := rigFor(t, "omp")
	r.Term.Show(r.Term.MustCapture("harness/engine/testdata/input/omp-idle.txt"))
	r.Term.Open(&Dialog{Screen: r.Term.MustCapture("detect/testdata/omp/real-allow-tool-80x24.txt")})
	r.Look()
	p := r.WaitPending(1, 2*time.Second)
	if err := r.Answer(p[0].RequestID, "approve"); err != nil {
		t.Fatalf("answer: %v", err)
	}
	if r.Term.Dialog() != nil {
		t.Fatal("the dialog is still up after the answer")
	}
	if len(r.Term.KeysSent()) == 0 {
		t.Fatal("no key reached the pane")
	}
	r.WaitPending(0, 2*time.Second)
	if res := r.Closed()[p[0].RequestID]; res != "phone" {
		t.Errorf("closed as %q, want phone", res)
	}
}
