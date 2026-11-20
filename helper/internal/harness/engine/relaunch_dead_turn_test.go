package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/adapters/hookstyle"
	"github.com/gettogo/togo/helper/internal/wire"

	_ "github.com/gettogo/togo/helper/internal/harness/adapters/claude"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/codex"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/kimi"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/omp"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/opencode"
	_ "github.com/gettogo/togo/helper/internal/harness/adapters/pi"
)

// deadTurnCase is one adapter family's mapper with a recorded session of
// the real agent: lines are its records in the order the feed gives them.
type deadTurnCase struct {
	name, mapper, file string
	// hook names the hook dialect the file's calls are normalised with.
	hook string
	// sse: the file is an event stream ("data: {...}" lines).
	sse bool
	// pre is read before file (the sessions table of a database).
	pre string
	// cwd is the session's folder when the file names one.
	cwd string
}

var deadTurnCases = []deadTurnCase{
	{name: "pi", mapper: "pi.session", file: "../adapters/pisession/testdata/session.jsonl"},
	{name: "omp", mapper: "omp.session", file: "../adapters/omp/testdata/session.jsonl"},
	{name: "kimi", mapper: "kimi.wire", file: "../adapters/kimi/testdata/wire-h1.jsonl"},
	{name: "vibe", mapper: "vibe.session", file: "../adapters/vibe/testdata/journal-tool.jsonl"},
	{name: "claude", mapper: "claude.transcript", file: "../adapters/claude/testdata/transcript-basic.jsonl"},
	{name: "goose", mapper: "goose.db", file: "../adapters/goose/testdata/messages.jsonl",
		pre: "../adapters/goose/testdata/sessions.jsonl", cwd: "/w/proj"},
	{name: "copilot", mapper: "copilot.hooks", file: "../adapters/hookstyle/testdata/copilot.jsonl", hook: "copilot"},
	{name: "cursor", mapper: "cursor.hooks", file: "../adapters/hookstyle/testdata/cursor.jsonl", hook: "cursor"},
	{name: "droid", mapper: "droid.hooks", file: "../adapters/hookstyle/testdata/droid.jsonl", hook: "droid"},
	{name: "amp", mapper: "amp.hooks", file: "../adapters/hookstyle/testdata/amp.jsonl", hook: "amp"},
	{name: "opencode", mapper: "opencode.events", file: "../adapters/opencode/testdata/events.sse", sse: true},
	{name: "codex", mapper: "codex.appserver", file: "../adapters/codex/testdata/appserver-turn.jsonl"},
}

// records reads the case's file as the feed delivers it.
func (c deadTurnCase) records(t *testing.T) []harness.Record {
	t.Helper()
	b, err := os.ReadFile(c.file)
	if err != nil {
		t.Fatal(err)
	}
	if c.pre != "" {
		pre, err := os.ReadFile(c.pre)
		if err != nil {
			t.Fatal(err)
		}
		b = append(append(pre, '\n'), b...)
	}
	t0 := time.Now().Add(-time.Hour)
	var out []harness.Record
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(nil, 8<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if c.sse {
			if !bytes.HasPrefix(line, []byte("data:")) {
				continue
			}
			line = bytes.TrimSpace(line[len("data:"):])
		}
		if len(line) == 0 {
			continue
		}
		data := json.RawMessage(append([]byte(nil), line...))
		if c.hook != "" {
			var call struct {
				Event   string          `json:"event"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(line, &call); err != nil {
				t.Fatal(err)
			}
			d, _ := hookstyle.For(c.hook)
			_, norm, err := hookstyle.Normalize(d, call.Event, call.Payload)
			if err != nil {
				t.Fatal(err)
			}
			data = norm
		}
		if c.mapper == "codex.appserver" {
			var m struct {
				Method string          `json:"method"`
				Result json.RawMessage `json:"result"`
			}
			_ = json.Unmarshal(line, &m)
			if m.Method == "" && len(m.Result) > 0 {
				data, _ = json.Marshal(map[string]any{"method": "thread/resume", "params": map[string]json.RawMessage{"result": m.Result}})
			}
		}
		out = append(out, harness.Record{Source: "file", At: t0.Add(time.Duration(len(out)) * time.Second), Data: data})
	}
	return out
}

// split finds where the recorded session is mid-turn: the first record
// after which the mapper says a turn runs. rest starts at the next prompt
// after that point (none when the file has no later turn).
func (c deadTurnCase) split(t *testing.T, recs []harness.Record) (mid int, rest int) {
	t.Helper()
	f, ok := harness.LookupMapper(c.mapper)
	if !ok {
		t.Fatalf("no mapper %s", c.mapper)
	}
	m := f(harness.Env{SessionID: "s_scratch0001", Cwd: c.cwd})
	mid, rest = -1, -1
	var first string
	turn := false
	for i, rec := range recs {
		items, _ := m.Map(rec)
		for _, it := range items {
			turn = turn || (it.Kind == harness.KindTurn && it.Turn != nil && it.Turn.State == harness.StateRunning)
		}
		if mid < 0 {
			if st := m.State(); st["working"] == "true" && (turn || st["turn"] != "") {
				mid = i + 1
				for _, it := range items {
					if it.Kind == harness.KindUser && first == "" {
						first = it.ID
					}
				}
			}
			continue
		}
		for _, it := range items {
			if it.Kind == harness.KindUser && it.Parent == "" && it.ID != first && first != "" {
				return mid, i
			}
		}
	}
	if mid < 0 {
		t.Fatalf("%s: the recorded session never has a turn running", c.name)
	}
	return mid, rest
}

// deadTurnEngine serves one session whose adapter reads the case's mapper
// from a single feed, the hub's status read from status.
func deadTurnEngine(t *testing.T, c deadTurnCase, id string, status *atomic.Value, done func(string, string, string)) (*Engine, *sess) {
	t.Helper()
	return deadTurnEngineWith(t, c, id, status, func(cfg *Config) { cfg.Done = done })
}

func deadTurnEngineWith(t *testing.T, c deadTurnCase, id string, status *atomic.Value, opts ...func(*Config)) (*Engine, *sess) {
	t.Helper()
	agent := "exit" + c.name
	if _, done := deadTurnAdapters.LoadOrStore(agent, true); !done {
		harness.RegisterAdapterFile(agent, []byte(`
adapter: `+agent+`
name: Exit `+c.name+`
revision: 1
match: { agents: [`+agent+`] }
capabilities: [chat, agents, background]
feeds:
  events: { hook: { events: [Event] }, mapper: `+c.mapper+`, stability: documented }
`))
	}
	e := statusEngine(t, id, &seqPane{mode: "a"}, status, func(cfg *Config) {
		cwd := cfg.Home
		if c.cwd != "" {
			cwd = c.cwd
		}
		cfg.Sessions = fakeSessions{id: {ID: id, Agent: agent, Managed: true, Cwd: cwd, Command: []string{agent}}}
		for _, o := range opts {
			o(cfg)
		}
	})
	s, err := e.served(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	s.waitReady(context.Background())
	return e, s
}

// deadTurnAdapters are the test adapters registered so far.
var deadTurnAdapters sync.Map

func feedRecords(s *sess, mapper string, recs []harness.Record) {
	for _, rec := range recs {
		s.record(s.ctx, mapper, rec)
	}
}

// hubSays sets the hub's status and runs what its session event runs.
func hubSays(e *Engine, s *sess, status *atomic.Value, v string) {
	status.Store(v)
	s.statusChanged()
}

func runningTurns(e *Engine, id string) []string {
	items, _, _ := e.log.Since(id, 0, 0)
	var out []string
	for _, it := range items {
		if it.Kind == harness.KindTurn && it.Turn != nil && it.Turn.State == harness.StateRunning {
			out = append(out, it.ID)
		}
	}
	return out
}

// deadTurns names the turns running now: by their turn id, from the chat
// and from what the mappers say.
func deadTurns(e *Engine, s *sess) map[string]bool {
	out := map[string]bool{}
	items, _, _ := e.log.Since(s.id, 0, 0)
	for _, it := range items {
		if it.Kind == harness.KindTurn && it.Turn != nil && it.Turn.State == harness.StateRunning {
			out[it.TurnID] = true
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.mappers {
		if st := m.State(); st["working"] == "true" && st["turn"] != "" {
			out[st["turn"]] = true
		}
	}
	return out
}

// checkDead fails for a turn item of a dead turn that is not interrupted.
func checkDead(t *testing.T, e *Engine, id string, dead map[string]bool) {
	t.Helper()
	items, _, _ := e.log.Since(id, 0, 0)
	for _, it := range items {
		if it.Kind == harness.KindTurn && it.Turn != nil && dead[it.TurnID] && it.Turn.State != harness.StateInterrupted {
			t.Errorf("the dead turn %s is %s, want interrupted", it.ID, it.Turn.State)
		}
		if it.Kind == harness.KindTool && it.Tool != nil && dead[it.TurnID] && it.Tool.State == harness.StateRunning {
			t.Errorf("the dead turn's tool %s still runs", it.ID)
		}
	}
}

func sessWorking(s *sess) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked()["working"]
}

// An agent that exits in the middle of a turn and is started again in the
// same pane must come back idle: the turn it died in is ended as
// interrupted, nothing of it counts as running, and no "finished" push is
// sent for it. When the relaunched agent goes on in the same conversation,
// the chat keeps its history, the dead turn stays interrupted (its next
// prompt must not close it as finished), and the new turn counts as it
// runs. Every adapter family is checked with a recorded session of its
// real agent.
func TestRelaunchEndsTheDeadTurn(t *testing.T) {
	for i, c := range deadTurnCases {
		t.Run(c.name, func(t *testing.T) {
			id := "s_deadturn" + string(rune('a'+i)) + "1"
			var status atomic.Value
			status.Store("working")
			var dones atomic.Int32
			e, s := deadTurnEngine(t, c, id, &status, func(string, string, string) { dones.Add(1) })
			recs := c.records(t)
			mid, rest := c.split(t, recs)
			feedRecords(s, c.mapper, recs[:mid])
			waitFor(t, func() bool { return s.sum.Load().Working })
			dead := deadTurns(e, s)
			if len(dead) == 0 {
				t.Fatalf("no turn running mid-turn")
			}
			before, _, _ := e.log.Since(id, 0, 0)

			hubSays(e, s, &status, wire.StatusEnded)
			hubSays(e, s, &status, "idle") // relaunched: the new run waits for a prompt
			if s.sum.Load().Working {
				t.Fatalf("the relaunched agent still reads working")
			}
			if w := sessWorking(s); w != "false" {
				t.Errorf("state working = %s after the relaunch", w)
			}
			checkDead(t, e, id, dead)
			if r := runningTurns(e, id); len(r) != 0 {
				t.Errorf("turns still running: %v", r)
			}
			if rest < 0 {
				return
			}

			// The relaunched agent goes on in the same conversation. The
			// records come from the same recording, so a mapper that links
			// records by parent may not take them; when it does, the new
			// turn counts as running.
			ran := false
			for _, rec := range recs[rest:] {
				feedRecords(s, c.mapper, []harness.Record{rec})
				ran = ran || sessWorking(s) == "true"
			}
			checkDead(t, e, id, dead)
			for _, it := range before {
				if _, ok := e.log.Get(id, it.ID); !ok {
					t.Errorf("%s left the chat: it started over", it.ID)
				}
			}
			if !ran {
				t.Logf("%s: the new turn's records were not taken", c.name)
				return
			}
			if sessWorking(s) == "false" {
				// The new turn ended: its done push comes, the dead turn's
				// (which would have been due first) never does.
				waitFor(t, func() bool { return dones.Load() >= 1 })
				if n := dones.Load(); n != 1 {
					t.Errorf("%d finished pushes, want the new turn's only", n)
				}
			}
		})
	}
}
