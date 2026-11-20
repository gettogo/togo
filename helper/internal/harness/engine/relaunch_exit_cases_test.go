package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/approval"
	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"

	_ "github.com/gettogo/togo/helper/internal/harness/adapters/aider"
)

// The daemon's approvals store closes an exited agent's approvals.
var _ exitedApprovals = (*approval.Store)(nil)

func caseNamed(t *testing.T, name string) deadTurnCase {
	t.Helper()
	for _, c := range deadTurnCases {
		if c.name == name {
			return c
		}
	}
	t.Fatalf("no case %s", name)
	return deadTurnCase{}
}

// An agent that finishes its turn and then exits leaves nothing to end:
// no turn turns interrupted, and nothing is held against the next run.
func TestExitAfterAFinishedTurnEndsNothing(t *testing.T) {
	for _, name := range []string{"copilot", "claude", "opencode"} {
		t.Run(name, func(t *testing.T) {
			c := caseNamed(t, name)
			id := "s_exitdone" + name[:2]
			var status atomic.Value
			status.Store("idle")
			e, s := deadTurnEngine(t, c, id, &status, nil)
			feedRecords(s, c.mapper, c.records(t))
			if w := sessWorking(s); w != "false" {
				t.Fatalf("%s: the recording ends mid-turn", name)
			}
			hubSays(e, s, &status, wire.StatusEnded)
			items, _, _ := e.log.Since(id, 0, 0)
			for _, it := range items {
				if it.Kind == harness.KindTurn && it.Turn != nil && it.Turn.State == harness.StateInterrupted {
					t.Errorf("finished turn %s shown interrupted", it.ID)
				}
			}
			s.mu.Lock()
			fence := s.exit
			s.mu.Unlock()
			if fence != nil {
				t.Errorf("the exit kept %+v", fence)
			}
		})
	}
}

// A turn the agent finished just before it exited, whose last records the
// feed delivers after the hub reported the exit, ends as the agent wrote:
// the end is dated before the exit and comes without a new prompt.
func TestTurnEndReadAfterTheExitStands(t *testing.T) {
	c := caseNamed(t, "kimi")
	id := "s_exitlate01"
	var status atomic.Value
	status.Store("working")
	e, s := deadTurnEngine(t, c, id, &status, nil)
	recs := c.records(t)
	mid, rest := c.split(t, recs)
	if rest < 0 {
		t.Fatal("the recording has one turn")
	}
	feedRecords(s, c.mapper, recs[:mid])
	dead := deadTurns(e, s)
	hubSays(e, s, &status, wire.StatusEnded)
	feedRecords(s, c.mapper, recs[mid:rest])
	items, _, _ := e.log.Since(id, 0, 0)
	ok := false
	for _, it := range items {
		if it.Kind == harness.KindTurn && it.Turn != nil && dead[it.TurnID] {
			ok = it.Turn.State == harness.StateOK
		}
	}
	if !ok {
		t.Errorf("the turn the agent finished before it exited is not ok")
	}
	hubSays(e, s, &status, "idle")
	if w := sessWorking(s); w != "false" {
		t.Errorf("working = %s after the relaunch", w)
	}
}

// A helper restarted between the exit and the relaunch reads the agent's
// files again and finds the dead turn open: what the exit ended is kept
// on disk, so the chat shows it interrupted and the relaunched agent idle.
func TestDeadTurnStaysEndedOverAHelperRestart(t *testing.T) {
	for _, name := range []string{"omp", "copilot", "opencode"} {
		t.Run(name, func(t *testing.T) {
			c := caseNamed(t, name)
			id := "s_exitrest" + name[:2]
			togo := t.TempDir()
			var status atomic.Value
			status.Store("working")
			withHome := func(cfg *Config) { cfg.TogoHome = togo }
			e, s := deadTurnEngineWith(t, c, id, &status, withHome)
			recs := c.records(t)
			mid, _ := c.split(t, recs)
			feedRecords(s, c.mapper, recs[:mid])
			dead := deadTurns(e, s)
			hubSays(e, s, &status, wire.StatusEnded)
			path := filepath.Join(togo, "run", "harness", id+".exit")
			waitFor(t, func() bool { _, err := os.Stat(path); return err == nil })
			e.Unload(id)

			status.Store("idle") // relaunched while the helper was down
			e2, s2 := deadTurnEngineWith(t, c, id, &status, withHome)
			// As the feed gives it at start: every record so far, each with
			// the time it first came (a hook's own time).
			var replay []json.RawMessage
			var at []time.Time
			for _, r := range recs[:mid] {
				replay, at = append(replay, r.Data), append(at, r.At)
			}
			s2.record(s2.ctx, c.mapper, harness.Record{Source: "file", At: time.Now(), Replay: replay, ReplayAt: at})
			if w := sessWorking(s2); w != "false" {
				s2.mu.Lock()
				for n, m := range s2.mappers {
					t.Logf("%s %v fence %+v", n, m.State(), s2.exit)
				}
				s2.mu.Unlock()
				t.Errorf("working = %s after the restart", w)
			}
			checkDead(t, e2, id, dead)
			if r := runningTurns(e2, id); len(r) != 0 {
				t.Errorf("turns running after the restart: %v", r)
			}
			// The session's removal takes the kept exit with it.
			e2.Forget(id)
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("the exit is still kept after the removal: %v", err)
			}
		})
	}
}

// Aider's history has no turn state: the hub alone says it works, and the
// mapper closes the open turn when the pane goes idle. An exit at work
// must end that turn interrupted, not as a turn that finished.
func TestAiderKilledTurnEndsInterrupted(t *testing.T) {
	b, err := os.ReadFile("../adapters/aider/testdata/chat.history.md")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	cut := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "#### answer briefly") {
			cut = i + 1 // the last prompt, before its reply
		}
	}
	raw, _ := json.Marshal(lines[:cut])
	c := deadTurnCase{name: "aider", mapper: "aider.history"}
	id := "s_exitaider1"
	var status atomic.Value
	status.Store("working")
	e, s := deadTurnEngine(t, c, id, &status, nil)
	s.record(s.ctx, c.mapper, harness.Record{Source: "file", At: time.Date(2026, 11, 7, 22, 33, 0, 0, time.UTC), Data: raw})
	running := runningTurns(e, id)
	if len(running) != 1 {
		t.Fatalf("running turns %v before the exit", running)
	}
	hubSays(e, s, &status, wire.StatusEnded)
	if st := turnState(e, id, running[0]); st != harness.StateInterrupted {
		t.Errorf("the killed turn is %s, want interrupted", st)
	}
	hubSays(e, s, &status, "idle")
	if st := turnState(e, id, running[0]); st != harness.StateInterrupted {
		t.Errorf("after the relaunch the killed turn is %s", st)
	}
	if s.sum.Load().Working {
		t.Error("working after the relaunch")
	}
}

func turnState(e *Engine, id, turn string) string {
	it, ok := e.log.Get(id, turn)
	if !ok || it.Turn == nil {
		return ""
	}
	return it.Turn.State
}

// approvalsSpy records AgentExited calls.
type approvalsSpy struct{ exited atomic.Int32 }

func (a *approvalsSpy) Pending(string) []wire.Approval                       { return nil }
func (a *approvalsSpy) Answer(context.Context, string, string, string) error { return nil }

func (a *approvalsSpy) AgentExited(context.Context, string, time.Time) { a.exited.Add(1) }

// The approvals the dead agent asked for are closed in the store, which
// withdraws them from the phone; the store is called away from the
// session's lock.
func TestExitClosesTheAgentsApprovals(t *testing.T) {
	c := caseNamed(t, "copilot")
	id := "s_exitappr01"
	var status atomic.Value
	status.Store("working")
	spy := &approvalsSpy{}
	e, s := deadTurnEngineWith(t, c, id, &status, func(cfg *Config) { cfg.Approvals = spy })
	recs := c.records(t)
	mid, _ := c.split(t, recs)
	feedRecords(s, c.mapper, recs[:mid])
	hubSays(e, s, &status, "idle")
	if n := spy.exited.Load(); n != 0 {
		t.Fatalf("approvals closed while the agent runs (%d)", n)
	}
	hubSays(e, s, &status, wire.StatusEnded)
	if n := spy.exited.Load(); n != 1 {
		t.Errorf("approvals closed %d times at the exit", n)
	}
}

// A background job Kimi started runs inside its process: the exit ends it,
// and a relaunched Kimi carries on in the same log, which never says the
// job died. A helper restart reading that log again must not show the job
// running.
func TestKilledJobStaysEndedOverAHelperRestart(t *testing.T) {
	c := caseNamed(t, "kimi")
	id := "s_exitjobkim"
	togo := t.TempDir()
	withHome := func(cfg *Config) { cfg.TogoHome = togo }
	var status atomic.Value
	status.Store("working")
	recs := c.records(t)
	f, _ := harness.LookupMapper(c.mapper)
	m := f(harness.Env{SessionID: "s_scratch0001"})
	mid := -1
	for i, r := range recs {
		_, ops := m.Map(r)
		for _, op := range ops {
			if op.Section == harness.SectionBackground && op.Row != nil && op.Row.State == harness.StateRunning {
				mid = i + 1
			}
		}
		if mid >= 0 {
			break
		}
	}
	if mid < 0 {
		t.Fatal("the recording starts no job")
	}
	e, s := deadTurnEngineWith(t, c, id, &status, withHome)
	feedRecords(s, c.mapper, recs[:mid])
	waitFor(t, func() bool { return s.sum.Load().Background == 1 })
	hubSays(e, s, &status, wire.StatusEnded)
	if n := s.sum.Load().Background; n != 0 {
		t.Fatalf("%d jobs running after the exit", n)
	}
	path := filepath.Join(togo, "run", "harness", id+".exit")
	waitFor(t, func() bool { _, err := os.Stat(path); return err == nil })
	e.Unload(id)

	status.Store("idle")
	_, s2 := deadTurnEngineWith(t, c, id, &status, withHome)
	var replay []json.RawMessage
	var at []time.Time
	for _, r := range recs[:mid] {
		replay, at = append(replay, r.Data), append(at, r.At)
	}
	s2.record(s2.ctx, c.mapper, harness.Record{Source: "file", At: time.Now(), Replay: replay, ReplayAt: at})
	if n := s2.sum.Load().Background; n != 0 {
		t.Errorf("%d jobs running after the restart", n)
	}
	if w := sessWorking(s2); w != "false" {
		t.Errorf("working = %s after the restart", w)
	}
}
