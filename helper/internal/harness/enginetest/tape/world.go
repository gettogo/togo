package tape

import (
	"bytes"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// Start is when every generated session begins: a fixed time, so the same
// scenario writes the same records.
var Start = time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)

// Skip is the error a scenario returns when it does not apply to the
// agent: the reason goes in the export table.
type Skip string

func (s Skip) Error() string { return string(s) }

// Seed is the generator seed for a scenario and agent: the same pair
// always gets the same one.
func Seed(scenario, agent string) int64 {
	h := fnv.New32a()
	h.Write([]byte(scenario + "/" + agent))
	return int64(h.Sum32())
}

// SessionID is the Togo session id a scenario's rig serves: fixed, so the
// tape names it literally.
func SessionID(scenario, agent string) string {
	h := fnv.New64a()
	h.Write([]byte(scenario + "/" + agent))
	n := h.Sum64()
	const alphabet = "abcdefghijklmnopqrstuvwxyz234567"
	b := make([]byte, 10)
	for i := range b {
		b[i] = alphabet[n%32]
		n /= 32
	}
	return "s_" + string(b)
}

// internalDir is helper/internal, found from the working directory.
func internalDir() string {
	dir, _ := os.Getwd()
	for d := dir; d != "/" && d != "."; d = filepath.Dir(d) {
		if filepath.Base(d) == "internal" && filepath.Base(filepath.Dir(d)) == "helper" {
			return d
		}
	}
	return filepath.Join(dir, "..", "..", "..", "..")
}

// idleScreens are real captures of an agent's idle screen with its input
// line (paths under helper/internal).
var idleScreens = map[string]string{
	"omp":    "harness/engine/testdata/input/omp-idle.txt",
	"claude": "harness/engine/testdata/input/claude-suggested-prompt.ansi",
}

// configs are an agent's real settings files from its adapter's testdata
// (path under harness/), put where the agent keeps them (path under home).
var configs = map[string]map[string]string{
	"omp": {"adapters/omp/testdata/config.yml": ".omp/agent/config.yml"},
}

// hookAgent reports that the agent's chat comes from hook calls.
func hookAgent(agent string) bool {
	return slices.Contains(hookstyle.Agents(), agent) && !openTurnAgent(agent)
}

// openTurnAgent reports an agent whose files cannot be cut in the middle
// of a turn: the live turn is written with its last tool call unanswered.
func openTurnAgent(agent string) bool {
	return slices.Contains([]string{"vibe", "goose", "crush"}, agent)
}

// dialogFor finds a real captured permission prompt of the agent in the
// detector's testdata; "" when none.
func dialogFor(agent string) string {
	dir := filepath.Join(internalDir(), "detect", "testdata", Base(agent))
	files, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	sort.Strings(files)
	var best string
	score := -1
	for _, f := range files {
		n := filepath.Base(f)
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
	return filepath.Join("detect", "testdata", Base(agent), filepath.Base(best))
}

// World is one scenario's session: the files a generator wrote, the rig
// that serves them and the recorder on its phone side.
type World struct {
	T        testing.TB
	Agent    string
	Scenario string
	Home     string
	Cwd      string
	// TogoHome and Bin are the rig's scratch folders.
	Rig    *enginetest.Rig
	Rec    *Recorder
	Layout *sessiongen.Layout

	// late is an agent whose feed attaches to the files that exist when
	// it starts (Vibe's journal): its turns are written after the rig is
	// up. pending holds them until then.
	late      bool
	pending   []sessiongen.Turn
	holdLater bool

	// p is the profile; held is the message of an OpenCode 2.x turn held
	// back (its Hold and Finish write the events themselves).
	p    sessiongen.Profile
	held string

	hooks     bool
	open      bool
	heldFrom  int
	heldLines []byte
}

// NewWorld writes the turns as the agent would and starts a rig over
// them. The app has not opened the session. A profile that cannot write
// the turns is a Skip.
func NewWorld(t testing.TB, scenario, agent string, turns []sessiongen.Turn) (*World, error) {
	t.Helper()
	p := sessiongen.For(agent)
	if p == nil {
		return nil, Skip("no session profile for " + agent)
	}
	if slices.Contains([]string{"crush", "goose"}, agent) && sessiongen.SQLiteBin() == "" {
		return nil, Skip("no sqlite3 program, which the " + agent + " profile needs to write its database")
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		return nil, err
	}
	cwd := filepath.Join(home, "proj")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		return nil, err
	}
	w := &World{T: t, p: p, Agent: agent, Scenario: scenario, Home: home, Cwd: cwd, hooks: hookAgent(agent), late: agent == "vibe"}
	w.mark(turns)
	if w.late {
		w.pending, turns = turns, nil
	}
	l, err := sessiongen.Write(home, p, sessiongen.Script{Seed: Seed(scenario, agent), Start: Start, Cwd: cwd, Turns: turns})
	if err != nil {
		return nil, Skip("the " + agent + " profile cannot write this session: " + err.Error())
	}
	w.Layout = l
	return w, nil
}

// mark gives the turns of a hook-driven agent their hook part.
func (w *World) mark(turns []sessiongen.Turn) {
	if !w.hooks {
		return
	}
	for i := range turns {
		if turns[i].Hook == nil {
			turns[i].Hook = &sessiongen.HookTurn{}
		}
	}
}

// Start starts the rig over the written files and delivers the hook calls
// of a hook-driven agent, but not those held back.
func (w *World) Start() {
	w.T.Helper()
	for rel, dst := range configs[w.Agent] {
		b, err := os.ReadFile(filepath.Join(internalDir(), "harness", rel))
		if err != nil {
			w.T.Fatal(err)
		}
		dst = filepath.Join(w.Home, dst)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			w.T.Fatal(err)
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			w.T.Fatal(err)
		}
	}
	w.Rig = enginetest.New(w.T, enginetest.Options{Agent: w.Agent, Home: w.Home, Cwd: w.Cwd, ID: SessionID(w.Scenario, w.Agent),
		Created: Start.Add(-30 * time.Second), NoRedact: true})
	if rel, ok := idleScreens[w.Agent]; ok {
		w.Rig.Term.Show(w.Rig.Term.MustCapture(rel))
	}
	w.Rec = NewRecorder(w.T, w.Rig)
	if w.late {
		// The feed attaches to the files that exist now; the agent writes
		// on after that.
		time.Sleep(1200 * time.Millisecond)
		w.Rig.Stop()
		if err := w.Layout.Append(w.pending...); err != nil {
			w.T.Fatalf("%s: %v", w.Agent, err)
		}
		if w.holdLater {
			if err := w.Hold(); err != nil {
				w.T.Fatalf("%s: %v", w.Agent, err)
			}
		} else {
			// The adapter reads the snapshot of the finished turns at the
			// journal's first record: a session with no open turn shows
			// nothing, so the last turn is left open, as the agent's is
			// while it works.
			tail := sessiongen.Turn{Say: 50, Tools: 1, Output: 80, Hook: &sessiongen.HookTurn{Pending: true}}
			if err := w.Layout.Append(tail); err != nil {
				w.T.Fatalf("%s: %v", w.Agent, err)
			}
			w.open = true
		}
		w.Rig.Start()
	}
	if w.hooks {
		end := len(w.Layout.Hooks)
		if w.heldFrom > 0 {
			end = w.heldFrom
		}
		w.Deliver(w.Layout.Hooks[:end]...)
	}
}

// Deliver runs hook calls as `togo hook <agent> <event>` sends them,
// stamped with the time the generator gave them.
func (w *World) Deliver(calls ...sessiongen.HookCall) {
	w.T.Helper()
	d, ok := hookstyle.For(w.Agent)
	if !ok {
		w.T.Fatalf("no hook dialect for %s", w.Agent)
	}
	for _, c := range calls {
		if err := c.Apply(); err != nil {
			w.T.Fatal(err)
		}
		f, err := hookstyle.Reduce(d, c.Event, c.JSON(), map[string]string{"TOGO_SESSION": w.Rig.ID})
		if err != nil {
			w.T.Fatalf("%s %s: %v", w.Agent, c.Event, err)
		}
		if len(f.Payload) == 0 {
			w.T.Fatalf("%s %s: the hook forwarded no payload", w.Agent, c.Event)
		}
		if len(c.Writes) == 0 {
			w.Rig.DeliverAt(f.Hook, f.Payload, c.At)
			continue
		}
		before := w.Rig.Cursor()
		w.Rig.DeliverAt(f.Hook, f.Payload, c.At)
		w.Rig.WaitHook(before, 15*time.Second)
	}
}

// Settle waits for the engine to have read everything written and for the
// state to stop changing.
func (w *World) Settle(limit time.Duration) enginetest.State {
	w.T.Helper()
	last := w.Layout.Truth.Last
	var ok func(enginetest.State) bool
	if last != "" && !w.hooks && w.Rig.API() == nil && w.Rig.Codex() == nil {
		ok = enginetest.Has(last)
	} else {
		ok = func(st enginetest.State) bool { return len(st.Items) > 0 }
	}
	return w.Rig.SettleUntil(limit, ok)
}

// SettleTurns waits until the engine has read the n-th generated prompt
// ("Step n: ...") and the state is quiet.
func (w *World) SettleTurns(limit time.Duration, n int) enginetest.State {
	w.T.Helper()
	prefix := "Step " + strconv.Itoa(n) + ":"
	return w.Rig.SettleUntil(limit, func(st enginetest.State) bool {
		for _, it := range st.Items {
			if it.User != nil && it.Parent == "" && strings.HasPrefix(it.User.Text, prefix) {
				return true
			}
		}
		return false
	})
}

// Append writes more turns, as the agent keeps working, and delivers
// their hook calls.
func (w *World) Append(turns ...sessiongen.Turn) error {
	w.T.Helper()
	w.mark(turns)
	from := len(w.Layout.Hooks)
	if err := w.Layout.Append(turns...); err != nil {
		return Skip("the " + w.Agent + " profile cannot write this turn: " + err.Error())
	}
	if w.hooks {
		w.Deliver(w.Layout.Hooks[from:]...)
	}
	return nil
}

// Hold writes one more turn and holds back its ending: for an agent that
// writes a file, every record after the turn's prompt; for a hook agent,
// the call that ends the turn and those after it. Call it before Start.
func (w *World) Hold() error {
	if w.late && w.Rig == nil {
		w.holdLater = true
		return nil
	}
	l := w.Layout
	if p, ok := w.p.(*sessiongen.OpenCode2); ok {
		msg, err := p.Hold(l, "Hold this turn")
		if err != nil {
			return Skip("the " + w.Agent + " profile cannot write a live turn: " + err.Error())
		}
		w.held = msg
		return nil
	}
	turn := sessiongen.Turn{Say: 50, Tools: 1, Output: 80}
	if w.hooks {
		turn.Hook = &sessiongen.HookTurn{}
	}
	if openTurnAgent(w.Agent) {
		turn.Hook = &sessiongen.HookTurn{Pending: true}
		if err := l.Append(turn); err != nil {
			return Skip("the " + w.Agent + " profile cannot write a live turn: " + err.Error())
		}
		w.open = true
		return nil
	}
	main := l.Main()
	before, _ := os.ReadFile(main)
	calls := len(l.Hooks)
	if err := l.Append(turn); err != nil {
		return Skip("the " + w.Agent + " profile cannot write a live turn: " + err.Error())
	}
	w.heldFrom = len(l.Hooks)
	if w.hooks {
		for i := calls; i < len(l.Hooks); i++ {
			if l.Hooks[i].End != "" {
				w.heldFrom = i
				break
			}
		}
		if w.heldFrom == len(l.Hooks) || w.heldFrom == calls {
			return Skip("the " + w.Agent + " live turn has no ending call to hold back")
		}
		return nil
	}
	after, err := os.ReadFile(l.Main())
	if err != nil {
		return err
	}
	if l.Main() != main || !bytes.HasPrefix(after, before) {
		// The agent rewrites its whole file: there is no record to hold
		// back, and the hub's status alone says it works.
		return nil
	}
	tail := after[len(before):]
	cut := bytes.IndexByte(tail, '\n') + 1
	if cut <= 0 || cut == len(tail) {
		return Skip("the " + w.Agent + " live turn is one record")
	}
	w.heldLines = tail[cut:]
	return os.WriteFile(main, after[:len(before)+cut], 0o644)
}

// EndTurn writes what the agent writes when its live turn ends.
func (w *World) EndTurn() {
	w.T.Helper()
	switch {
	case w.held != "":
		if err := w.p.(*sessiongen.OpenCode2).Finish(w.Layout, w.held); err != nil {
			w.T.Fatalf("end of the live turn: %v", err)
		}
		w.held = ""
	case w.hooks:
		w.Deliver(w.Layout.Hooks[w.heldFrom:]...)
		w.heldFrom = len(w.Layout.Hooks)
	case w.open:
		if err := w.Layout.Append(sessiongen.Turn{Prompt: sessiongen.Continue, Say: 40}); err != nil {
			w.T.Fatalf("end of the live turn: %v", err)
		}
		w.open = false
	case len(w.heldLines) > 0:
		f, err := os.OpenFile(w.Layout.Main(), os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			w.T.Fatal(err)
		}
		defer f.Close()
		if _, err := f.Write(w.heldLines); err != nil {
			w.T.Fatal(err)
		}
		w.heldLines = nil
	}
}

// Working puts the session to work: the hub sees the agent working and
// the engine has read the live turn.
func (w *World) Working() {
	w.T.Helper()
	w.Rig.SetHubStatus("working")
	w.Rig.SettleUntil(20*time.Second, func(st enginetest.State) bool { return st.Mission.Summary.Working })
}

// Idle ends the live turn and waits until the session is idle.
func (w *World) Idle() {
	w.T.Helper()
	w.EndTurn()
	w.Rig.SetHubStatus("idle")
	w.Rig.SettleUntil(20*time.Second, func(st enginetest.State) bool { return !st.Mission.Summary.Working })
}

// Scenario scratch folders, for the cleaner.
func (w *World) Scratch() map[string]string {
	m := map[string]string{w.Home: FixtureHome}
	if w.Rig != nil {
		m[w.Rig.TogoHome] = FixtureRun + "/togo"
	}
	return m
}

// Quiet waits until nothing the app would be told changes: the newest
// Seq, the mission's revision and the descriptor (its mode comes from the
// screen a moment after the first read) stay the same for half a second.
func (w *World) Quiet() {
	w.T.Helper()
	// A feed looks for new files every couple of seconds (Copilot's log
	// of replies is one), and an agent's mode comes from its screen a
	// moment after the first read: wait two seconds, three for hooks.
	need := 20
	if w.hooks {
		need = 30
	}
	last, same := "", 0
	for deadline := time.Now().Add(30 * time.Second); same < need && time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		d := w.Rig.Describe()
		key := fmt.Sprint(w.Rig.Cursor(), "|", w.Rig.Read().Mission.Rev, "|", d.Mode, "|", d.Capabilities, "|", d.Degraded)
		if key == last {
			same++
		} else {
			same = 0
		}
		last = key
	}
}
