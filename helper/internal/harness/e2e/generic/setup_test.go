// Package generic runs the same scenarios for every session generator
// profile: each lifecycle event on a session holding live state, sending,
// queueing, taking back, interrupting, and many requests at once. A new
// profile in sessiongen's registry gets them all.
package generic

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// optOut names profiles a scenario does not run for, with the reason.
// A profile is never skipped silently: an entry needs a reason.
var optOut = map[string]string{}

// idleScreens are real captures of an agent's idle screen with its input
// line. Agents without one show an empty pane: no capture is invented.
var idleScreens = map[string]string{
	"omp":    "harness/engine/testdata/input/omp-idle.txt",
	"claude": "harness/engine/testdata/input/claude-suggested-prompt.ansi",
}

// configs are an agent's real settings files from its adapter's
// testdata (path under harness/), put where the agent keeps them (path
// under home): actions that read them (omp's model roles) have what a
// user's machine has.
var configs = map[string]map[string]string{
	"omp": {"adapters/omp/testdata/config.yml": ".omp/agent/config.yml"},
}

// dialogFor finds a real captured permission prompt of the agent in the
// detector's testdata, preferring the 80-column captures; "" when none.
func dialogFor(agent string) string {
	dir := filepath.Join("..", "..", "..", "detect", "testdata", agent)
	files, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	sort.Strings(files)
	var best string
	score := -1
	for _, f := range files {
		n := filepath.Base(f)
		// A "-moved" capture is the screen after a cursor key: the prompt
		// as first drawn is the one to answer.
		if strings.HasPrefix(n, "negative") || strings.Contains(n, "-moved") || !strings.ContainsAny(n, "-") {
			continue
		}
		s := 0
		for _, w := range []string{"permission", "allow", "approval", "run-command"} {
			if strings.Contains(n, w) {
				s = 2
			}
		}
		if s == 0 {
			continue
		}
		if strings.HasPrefix(n, "real-") {
			s++
		}
		if strings.Contains(n, "80") {
			s++
		}
		if s > score {
			best, score = f, s
		}
	}
	if best == "" {
		return ""
	}
	return filepath.Join("detect", "testdata", agent, filepath.Base(best))
}

// profiles lists the registered profiles a scenario runs for.
func profiles(t *testing.T) []string {
	var out []string
	for _, n := range sessiongen.Names() {
		if why, ok := optOut[n]; ok {
			if strings.TrimSpace(why) == "" {
				t.Errorf("%s opts out of the generic scenarios with no reason", n)
			}
			continue
		}
		out = append(out, n)
	}
	return out
}

// live is a session of one agent holding live state.
type live struct {
	t      *testing.T
	agent  string
	r      *enginetest.Rig
	l      *sessiongen.Layout
	hooks  bool // its chat comes from hooks
	open   bool // the live turn is left open in the files, not held back
	agents bool // a subagent is still running
	// approval is the open approval, "" when the agent has no captured prompt.
	approval string
	// queued is the prompt waiting behind the running turn.
	queued     string
	queuedText string
	phone      *enginetest.Client
	// heldFrom is the first hook call held back; heldLines the records.
	heldFrom  int
	heldLines []byte
}

// script tries the richest script the profile can write: two finished
// turns and a turn with a background batch of subagents, one still at
// work. A profile that cannot write subagents gets the turns alone.
func script(t *testing.T, agent string) (string, string, time.Time, *sessiongen.Layout, bool) {
	t.Helper()
	tries := []struct {
		turns  []sessiongen.Turn
		agents bool
	}{
		{[]sessiongen.Turn{{Say: 80, Tools: 1, Output: 120}, {Say: 60},
			{Say: 40, Agents: &sessiongen.Batch{Size: 2, Steps: 1, Background: true, Unreported: 1}}}, true},
		{[]sessiongen.Turn{{Say: 80, Tools: 1, Output: 120}, {Say: 60}}, false},
	}
	for _, try := range tries {
		home, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		cwd := filepath.Join(home, "proj")
		if err := os.MkdirAll(cwd, 0o755); err != nil {
			t.Fatal(err)
		}
		start := time.Now().Add(-10 * time.Minute).UTC().Truncate(time.Second)
		for i := range try.turns {
			if hookAgent(agent) {
				try.turns[i].Hook = &sessiongen.HookTurn{}
			}
		}
		l, err := sessiongen.Write(home, sessiongen.For(agent), sessiongen.Script{Seed: 7, Start: start, Cwd: cwd, Turns: try.turns})
		if err == nil {
			return home, cwd, start, l, try.agents
		}
	}
	t.Fatalf("%s: the profile writes no session", agent)
	return "", "", time.Time{}, nil, false
}

// hookAgent reports that the agent's chat comes from hook calls. Vibe has a
// hook dialect, but Togo installs none for it: its chat is the journal.
func hookAgent(agent string) bool {
	return slices.Contains(hookstyle.Agents(), agent) && !openTurnAgent(agent)
}

// openTurnAgent reports an agent whose files cannot be cut in the middle of
// a turn (a database, or a journal folded into a snapshot after each
// turn): the live turn is written with its last tool call unanswered
// (Hook.Pending), and ends with the profile's Continue turn.
func openTurnAgent(agent string) bool {
	return slices.Contains([]string{"vibe", "goose", "crush"}, agent)
}

// start builds the session: written, read by the engine, the pane on its
// idle screen, and the phone's chat open. A last turn is written whose
// ending is held back (heldLines, heldCalls): the agent is at work on it
// until endTurn writes the rest.
func start(t *testing.T, agent string) *live {
	t.Helper()
	home, cwd, begun, l, agents := script(t, agent)
	lv := &live{t: t, agent: agent, l: l, hooks: hookAgent(agent), agents: agents}
	lv.holdTurn()
	for rel, dst := range configs[agent] {
		b, err := os.ReadFile(filepath.Join("..", "..", rel))
		if err != nil {
			t.Fatal(err)
		}
		dst = filepath.Join(home, dst)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := enginetest.New(t, enginetest.Options{Agent: agent, Home: home, Cwd: cwd, ID: enginetest.NewID(),
		Created: begun.Add(-30 * time.Second), NoRedact: true})
	lv.r = r
	if rel, ok := idleScreens[agent]; ok {
		r.Term.Show(r.Term.MustCapture(rel))
	}
	lv.deliver(l.Hooks[:lv.heldFrom]...)
	// The pane changes while the agent works: the hub says working. An
	// agent whose file says the turn runs agrees; one whose file has no
	// turn state (Cline, Aider) is working by this alone.
	r.SetHubStatus("working")
	r.SettleUntil(20*time.Second, func(st enginetest.State) bool { return st.Mission.Summary.Working })
	lv.phone = r.Phone()
	return lv
}

// holdTurn writes one more turn and holds back its ending: for an agent
// that writes a file, every record after the turn's prompt; for a hook
// agent, the call that ends the turn and those after it.
func (lv *live) holdTurn() {
	lv.t.Helper()
	l := lv.l
	turn := sessiongen.Turn{Say: 50, Tools: 1, Output: 80}
	if lv.hooks {
		turn.Hook = &sessiongen.HookTurn{}
	}
	if openTurnAgent(lv.agent) {
		turn.Hook = &sessiongen.HookTurn{Pending: true}
		if err := l.Append(turn); err != nil {
			lv.t.Fatalf("live turn: %v", err)
		}
		lv.open = true
		return
	}
	main := l.Main()
	before, _ := os.ReadFile(main)
	calls := len(l.Hooks)
	if err := l.Append(turn); err != nil {
		lv.t.Fatalf("live turn: %v", err)
	}
	lv.heldFrom = len(l.Hooks)
	if lv.hooks {
		for i := calls; i < len(l.Hooks); i++ {
			if l.Hooks[i].End != "" {
				lv.heldFrom = i
				break
			}
		}
		if lv.heldFrom == len(l.Hooks) || lv.heldFrom == calls {
			lv.t.Fatalf("%s: the live turn has no ending call to hold back", lv.agent)
		}
		return
	}
	after, err := os.ReadFile(l.Main())
	if err != nil {
		lv.t.Fatal(err)
	}
	if l.Main() != main || !bytes.HasPrefix(after, before) {
		// The agent rewrites its whole file (Cline's messages document):
		// there is no record to hold back. Such a file has no turn state;
		// the hub says the agent is at work until endTurn's idle.
		return
	}
	tail := after[len(before):]
	cut := bytes.IndexByte(tail, '\n') + 1
	if cut <= 0 || cut == len(tail) {
		lv.t.Fatalf("%s: the live turn wrote one record", lv.agent)
	}
	lv.heldLines = tail[cut:]
	if err := os.WriteFile(main, after[:len(before)+cut], 0o644); err != nil {
		lv.t.Fatal(err)
	}
}

// endTurn writes what the agent writes when its turn ends.
func (lv *live) endTurn() {
	lv.t.Helper()
	if lv.hooks {
		lv.deliver(lv.l.Hooks[lv.heldFrom:]...)
		lv.heldFrom = len(lv.l.Hooks)
		return
	}
	if lv.open {
		if err := lv.l.Append(sessiongen.Turn{Prompt: sessiongen.Continue, Say: 40}); err != nil {
			lv.t.Fatalf("end of the live turn: %v", err)
		}
		lv.open = false
		return
	}
	f, err := os.OpenFile(lv.l.Main(), os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		lv.t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(lv.heldLines); err != nil {
		lv.t.Fatal(err)
	}
	lv.heldLines = nil
}

// deliver runs hook calls as `togo hook <agent> <event>` sends them:
// reduced by the agent's dialect, through local.hook.
func (lv *live) deliver(calls ...sessiongen.HookCall) {
	lv.t.Helper()
	if len(calls) == 0 {
		return
	}
	d, ok := hookstyle.For(lv.agent)
	if !ok {
		lv.t.Fatalf("no hook dialect for %s", lv.agent)
	}
	env := map[string]string{"TOGO_SESSION": lv.r.ID}
	for _, c := range calls {
		if err := c.Apply(); err != nil {
			lv.t.Fatal(err)
		}
		f, err := hookstyle.Reduce(d, c.Event, c.JSON(), env)
		if err != nil {
			lv.t.Fatalf("%s %s: %v", lv.agent, c.Event, err)
		}
		before := lv.r.Cursor()
		if err := lv.r.Hook(f); err != nil {
			lv.t.Fatalf("local.hook %s: %v", c.Event, err)
		}
		lv.r.WaitCursor(before, 2*time.Second)
	}
}

// makeLive puts the session to work: the hub sees the agent working, a
// prompt is queued behind the turn, and the agent's permission prompt
// (a real capture) is up and reported to the phone.
func (lv *live) makeLive() {
	lv.t.Helper()
	r := lv.r
	r.SetHubStatus("working")
	lv.queuedText = "Queued while the agent works"
	res, err := r.Send(lv.queuedText, harness.SendQueue)
	if err != nil {
		lv.t.Fatalf("queue: %v", err)
	}
	if res.Item.User == nil || !res.Item.User.Queued {
		lv.t.Fatalf("the prompt was not queued: %+v", res.Item)
	}
	lv.queued = res.Item.ID
	if rel := dialogFor(lv.agent); rel != "" {
		r.Term.Open(&enginetest.Dialog{Screen: r.Term.MustCapture(rel)})
		r.Look()
		lv.approval = r.WaitPending(1, 3*time.Second)[0].RequestID
	}
}

// eventually waits for ok, failing with what after limit.
func eventually(t *testing.T, limit time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// item is the newest copy of an item in a read.
func item(st enginetest.State, id string) (harness.Item, bool) {
	var out harness.Item
	found := false
	for _, it := range st.Items {
		if it.ID == id && (!found || it.Seq > out.Seq) {
			out, found = it, true
		}
	}
	return out, found
}

// nothingRunning: no turn, tool, subagent or job is running, and the
// summary does not say working.
func nothingRunning(t *testing.T, st enginetest.State) {
	t.Helper()
	for _, it := range st.Items {
		var state string
		switch {
		case it.Turn != nil:
			state = it.Turn.State
		case it.Tool != nil:
			state = it.Tool.State
		case it.Agent != nil:
			state = it.Agent.State
		case it.Background != nil:
			state = it.Background.State
		}
		if latest, _ := item(st, it.ID); latest.Seq == it.Seq && (state == harness.StateRunning || state == harness.StateWaiting) {
			t.Errorf("%s (%s) is still %s", it.ID, it.Kind, state)
		}
	}
	if st.Mission.Summary.Working {
		t.Error("the summary still says working")
	}
}

// approvalClosed: the approval is no longer open, closed with one of res.
func (lv *live) approvalClosed(res ...string) {
	lv.t.Helper()
	if lv.approval == "" {
		return
	}
	eventually(lv.t, 3*time.Second, "the approval to close", func() bool { return len(lv.r.Pending()) == 0 })
	if got := lv.r.Closed()[lv.approval]; !slices.Contains(res, got) {
		lv.t.Errorf("the approval closed as %q, want one of %v", got, res)
	}
}

// idle ends the live turn and waits until the session is idle.
func (lv *live) idle() {
	lv.t.Helper()
	lv.endTurn()
	lv.r.SetHubStatus("idle")
	lv.r.SettleUntil(15*time.Second, func(st enginetest.State) bool { return !st.Mission.Summary.Working })
}
