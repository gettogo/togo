package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gettogo/togo/helper/internal/api"
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/e2e/support"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// compactThread compacts from the phone and returns the thread the call
// named: the thread the helper takes as the one on screen.
func (c *codexSession) compactThread(t *testing.T) string {
	t.Helper()
	before := len(c.s.CallsOf("thread/compact/start"))
	if res, err := c.r.Act("compact", "", map[string]any{}); err != nil || res.Result != harness.ResultApplied {
		t.Fatalf("compact: %+v %v", res, err)
	}
	calls := c.s.CallsOf("thread/compact/start")
	if len(calls) != before+1 {
		t.Fatalf("compact made %d calls", len(calls)-before)
	}
	var p map[string]string
	_ = json.Unmarshal(calls[len(calls)-1].Params, &p)
	if len(p) != 1 {
		t.Errorf("compact params %v, want the thread alone", p)
	}
	return p["threadId"]
}

// (d) A new thread started at the desk, then a thread resumed from the
// phone: after each, a prompt from the phone is typed into the terminal
// and shown, and the helper's calls name the thread the terminal shows.
// The chat starts over on the new thread. Resuming the first thread at
// the desk is TestKnownFault_CodexDeskResumeOfAnEarlierThread.
func TestCodexNewAndResume(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "codex/lifecycle/new_conversation", "codex/capability/resume")
	c := codexStart(t, 91, nil, sessiongen.Turn{Say: 20})
	support.SettleUntil(c.r, 1)
	c.joined(t)
	c.answerPrompts()
	old := c.p.Thread()
	if got := c.compactThread(t); got != old {
		t.Fatalf("compact named %q, the terminal shows %q", got, old)
	}
	sendAndShow := func(where, thread string) {
		t.Helper()
		text := "From the phone, " + where
		if _, err := c.r.Send(text, harness.SendNow); err != nil {
			t.Fatalf("%s: send: %v", where, err)
		}
		st := c.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return len(promptsOn(st, text)) == 1 && !st.Mission.Summary.Working })
		if n := len(promptsOn(st, text)); n != 1 {
			t.Errorf("%s: the prompt is shown %d times", where, n)
		}
		if got := c.compactThread(t); got != thread {
			t.Errorf("%s: the helper names thread %q, the terminal shows %q", where, got, thread)
		}
	}
	// /new at the desk, and a prompt there.
	c.add(t, sessiongen.Turn{New: true, Say: 10})
	fresh := c.p.Thread()
	c.r.SettleUntil(support.Quick, support.HasPrompt(2))
	sendAndShow("after /new", fresh)
	// Resume from the phone: Codex restarts on the thread.
	if res, err := c.r.Act("resume", "", map[string]any{"id": fresh}); err != nil || res.Result != harness.ResultRelaunched {
		t.Fatalf("resume: %+v %v", res, err)
	}
	if got := c.r.Relaunches(); !slices.Equal(got[len(got)-1], []string{"codex", "resume", fresh}) {
		t.Errorf("relaunched %q", got[len(got)-1])
	}
	c.p.Resume(fresh)
	c.joined(t)
	sendAndShow("after resuming from the phone", fresh)
}

// (h) Model, effort and mode from the phone. Model and effort restart
// the terminal on the same thread with the flags; the mode is one
// config/value/write of approval_policy, nothing else, and the user's
// config file is left as it was (Codex writes its own config). The
// status follows the thread's settings.
func TestCodexSettings(t *testing.T) {
	t.Parallel()
	coverage.Covers(t, "generated", "codex/capability/model", "codex/capability/effort", "codex/capability/mode",
		"codex/capability/compact")
	c := codexStart(t, 93, nil, sessiongen.Turn{Say: 20})
	cfg := filepath.Join(c.e.Home, ".codex", "config.toml")
	user := []byte("model = \"gpt-6.1-sol\"\napproval_policy = \"on-request\"\n\n[projects.\"/work/app\"]\ntrust_level = \"trusted\"\n")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, user, 0o600); err != nil {
		t.Fatal(err)
	}
	support.SettleUntil(c.r, 1)
	c.joined(t)
	thread := c.p.Thread()

	if got := c.compactThread(t); got != thread {
		t.Errorf("compact named %q, want %q", got, thread)
	}
	// Mode.
	res, err := c.r.Act("set_mode", "", map[string]any{"id": "never"})
	if err != nil || res.Result != harness.ResultApplied || res.Message == "" {
		t.Fatalf("set_mode: %+v %v", res, err)
	}
	writes := c.s.CallsOf("config/value/write")
	if len(writes) != 1 || strings.ReplaceAll(string(writes[0].Params), " ", "") != `{"keyPath":"approval_policy","mergeStrategy":"replace","value":"never"}` {
		t.Errorf("mode writes %+v", writes)
	}
	if b, _ := os.ReadFile(cfg); string(b) != string(user) {
		t.Errorf("the user's config changed:\n%s", b)
	}
	if entries, _ := os.ReadDir(filepath.Dir(cfg)); len(entries) != 1 {
		t.Errorf("the helper wrote into Codex's folder: %v", entries)
	}
	// Model, then effort: the terminal restarts on the thread.
	if res, err := c.r.Act("set_model", "", map[string]any{"id": "gpt-b"}); err != nil || res.Result != harness.ResultRelaunched {
		t.Fatalf("set_model: %+v %v", res, err)
	}
	rel := c.r.Relaunches()
	if want := []string{"codex", "resume", thread, "-m", "gpt-b", "-c", "model_reasoning_effort=" + sessiongen.CodexEffort}; !slices.Equal(rel[len(rel)-1], want) {
		t.Errorf("set_model relaunched %q, want %q", rel[len(rel)-1], want)
	}
	// The restarted terminal's next turn carries its flags: the server
	// says so (appserver-overrides.jsonl:11).
	c.joined(t)
	c.s.Append(sessiongen.CodexSettings(thread, c.e.Cwd, "gpt-b", sessiongen.CodexEffort, "on-request"))
	st := c.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return support.StatusOf(st).Model == "gpt-b" })
	if s := support.StatusOf(st); s.Model != "gpt-b" {
		t.Errorf("status %+v", s)
	}
	if res, err := c.r.Act("set_effort", "", map[string]any{"id": "high"}); err != nil || res.Result != harness.ResultRelaunched {
		t.Fatalf("set_effort: %+v %v", res, err)
	}
	rel = c.r.Relaunches()
	if want := []string{"codex", "resume", thread, "-c", "model_reasoning_effort=high", "-m", "gpt-b"}; !slices.Equal(rel[len(rel)-1], want) {
		t.Errorf("set_effort relaunched %q, want %q", rel[len(rel)-1], want)
	}
	// The pickers list the server's models and the efforts of the model.
	c.joined(t)
	models, err := c.r.Screen("model", nil)
	if err != nil {
		t.Fatalf("model screen: %v", err)
	}
	if b, _ := json.Marshal(models); !strings.Contains(string(b), "gpt-a") || !strings.Contains(string(b), "gpt-b") {
		t.Errorf("model screen %s", b)
	}
	efforts, err := c.r.Screen("effort", nil)
	if b, _ := json.Marshal(efforts); err != nil || !strings.Contains(string(b), "medium") || !strings.Contains(string(b), "high") {
		t.Errorf("effort screen %s %v", b, err)
	}
}

// (j) A Codex older than the adapter's versions (codex.yaml:26): the
// engine starts no app-server and gives the terminal no --remote, and
// opening the chat or sending is refused as a fallback to the terminal
// (-32008), as the product promises: the app shows the terminal. A newer Codex is served as the
// newest known one (engine/endpoint.go:133 starts the endpoint for it).
func TestCodexVersionOutsideRange(t *testing.T) {
	t.Parallel()
	old := codexStart(t, 95, func(o *enginetest.Options) { o.Version = "codex-cli 0.143.0" }, sessiongen.Turn{Say: 20})
	if args := old.r.CodexLaunch(); len(args) != 0 {
		t.Errorf("the terminal was given %q", args)
	}
	if pids := old.r.AppServerPIDs(); len(pids) != 0 {
		t.Errorf("an app-server was started: %v", pids)
	}
	var fallback *harness.FallbackError
	if _, err := old.r.Engine.Open(context.Background(), support.DropSub{DoneCh: make(chan struct{})}, old.r.ID, 0, 1); !errors.As(err, &fallback) {
		t.Errorf("open gave %v, want the terminal fallback", err)
	}
	_, err := old.r.Send("Typed in the terminal view", harness.SendNow)
	var ce *api.CodeError
	if !errors.As(err, &ce) || ce.Code != -32008 {
		t.Errorf("send gave %v, want the terminal fallback (-32008)", err)
	}
	if n := len(old.s.Calls()); n != 0 {
		t.Errorf("%d calls reached a server that is not there", n)
	}

	newer := codexStart(t, 96, func(o *enginetest.Options) { o.Version = "codex-cli 0.168.0" }, sessiongen.Turn{Say: 20})
	if args := newer.r.CodexLaunch(); len(args) != 2 {
		t.Errorf("a newer Codex was given %q", args)
	}
	support.SettleUntil(newer.r, 1)
}

// /resume at the desk of the thread the helper joined before /new: the
// server has that thread loaded already, so the helper's feed (one join
// per thread and connection, source/rpc/attach.go) never lists it again.
// The mapper made for the new conversation has never seen it described,
// so its turn is not taken as top level (adapters/codex/appserver.go
// turnStarted) and the chat stays on the /new thread: the helper's calls
// (compact, interrupt) name that thread while the terminal shows the
// resumed one.
//
// Correct: the helper follows the thread the terminal shows. This test
// passes while the fault is present; when it fails the fault is fixed:
// make it an ordinary test and claim codex/lifecycle/resume.
func TestKnownFault_CodexDeskResumeOfAnEarlierThread(t *testing.T) {
	t.Parallel()
	c := codexStart(t, 91, nil, sessiongen.Turn{Say: 20})
	support.SettleUntil(c.r, 1)
	c.joined(t)
	c.answerPrompts()
	old := c.p.Thread()
	c.add(t, sessiongen.Turn{New: true, Say: 10})
	fresh := c.p.Thread()
	c.r.SettleUntil(support.Quick, support.HasPrompt(2))
	c.s.Join(old)
	c.p.Resume(old)
	c.add(t, sessiongen.Turn{Say: 10})
	c.r.SettleUntil(support.Quick, func(st enginetest.State) bool { return !st.Mission.Summary.Working })
	switch got := c.compactThread(t); got {
	case fresh:
	case old:
		t.Fatal("the fault is fixed: the helper names the thread resumed at the desk")
	default:
		t.Fatalf("the helper names thread %q, neither of the two", got)
	}
}
