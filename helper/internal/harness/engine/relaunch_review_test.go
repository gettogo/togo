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

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/wire"
)

// replayOf is the records as a feed gives them when it starts.
func replayOf(recs []harness.Record) harness.Record {
	var data []json.RawMessage
	var at []time.Time
	for _, r := range recs {
		data, at = append(data, r.Data), append(at, r.At)
	}
	return harness.Record{Source: "file", At: time.Now(), Replay: data, ReplayAt: at}
}

// An agent that exited before the session was served (it crashed while
// the helper was down, or while the helper read a long file after an
// update) has its turn ended once the files are read, and comes back
// idle when relaunched.
func TestExitBeforeTheSessionIsServed(t *testing.T) {
	for _, name := range []string{"omp", "copilot"} {
		t.Run(name, func(t *testing.T) {
			c := caseNamed(t, name)
			id := "s_exitearl" + name[:2]
			var status atomic.Value
			status.Store(wire.StatusEnded)
			e, s := deadTurnEngine(t, c, id, &status, nil)
			recs := c.records(t)
			mid, _ := c.split(t, recs)
			s.record(s.ctx, c.mapper, replayOf(recs[:mid]))
			dead := deadTurns(e, s)
			hubSays(e, s, &status, "idle")
			if w := sessWorking(s); w != "false" {
				t.Errorf("working = %s after the relaunch", w)
			}
			if r := runningTurns(e, id); len(r) != 0 {
				t.Errorf("turns running: %v", r)
			}
			checkDead(t, e, id, dead)
		})
	}
}

// A helper down for both the exit and the relaunch never hears of the
// exit: the turn its files show running began before the agent's process
// did, so it cannot run in it, and it ends.
func TestTurnOlderThanTheAgentProcessEnds(t *testing.T) {
	c := caseNamed(t, "omp")
	id := "s_exitprocom"
	var status atomic.Value
	status.Store("idle")
	pid := os.Getpid()
	e, s := deadTurnEngineWith(t, c, id, &status, func(cfg *Config) {
		cfg.PanePIDs = func(context.Context, string) ([]int, error) { return []int{pid}, nil }
	})
	recs := c.records(t)
	mid, _ := c.split(t, recs)
	// The recording's turn is days older than this test's process.
	s.record(s.ctx, c.mapper, replayOf(recs[:mid]))
	waitFor(t, func() bool { return len(runningTurns(e, id)) == 0 && sessWorking(s) == "false" })
}

// The check stands aside once a live record came after the process
// started: the turn may be the new run's own.
func TestProcessCheckTrustsLiveRecords(t *testing.T) {
	c := caseNamed(t, "omp")
	id := "s_exitprocli"
	var status atomic.Value
	status.Store("idle")
	pid := os.Getpid()
	e, s := deadTurnEngineWith(t, c, id, &status, func(cfg *Config) {
		cfg.PanePIDs = func(context.Context, string) ([]int, error) { return []int{pid}, nil }
	})
	recs := c.records(t)
	mid, _ := c.split(t, recs)
	feedRecords(s, c.mapper, recs[:mid])
	s.checkRunStart()
	if len(runningTurns(e, id)) == 0 || sessWorking(s) != "true" {
		t.Error("a turn fed live was ended")
	}
}

// The relaunched agent going on with the very turn it died in (a resumed
// turn, tool calls with no new prompt) reads working, with its tools
// running.
func TestSameTurnGoesOnAfterTheRelaunch(t *testing.T) {
	c := caseNamed(t, "copilot")
	id := "s_exitsameco"
	var status atomic.Value
	status.Store("working")
	e, s := deadTurnEngine(t, c, id, &status, nil)
	recs := c.records(t)
	mid, _ := c.split(t, recs)
	feedRecords(s, c.mapper, recs[:mid])
	dead := deadTurns(e, s)
	hubSays(e, s, &status, wire.StatusEnded)
	hubSays(e, s, &status, "idle")
	if sessWorking(s) != "false" {
		t.Fatal("working after the relaunch")
	}
	feedRecords(s, c.mapper, recs[mid:mid+1]) // the turn's next tool call
	if w := sessWorking(s); w != "true" {
		t.Errorf("working = %s once the turn went on", w)
	}
	items, _, _ := e.log.Since(id, 0, 0)
	tools, turns := 0, 0
	for _, it := range items {
		if !dead[it.TurnID] {
			continue
		}
		if it.Kind == harness.KindTool && it.Tool.State == harness.StateRunning {
			tools++
		}
		if it.Kind == harness.KindTurn && it.Turn.State == harness.StateRunning {
			turns++
		}
	}
	if tools == 0 || turns != 1 {
		t.Errorf("%d tools and %d turns running in the turn that went on", tools, turns)
	}
}

// A mapper that numbers its items afresh (Codex hooks without a turn id:
// turn-1, turn-2) reuses an id the exit ended: the relaunched agent's live
// item of that id runs.
func TestReusedIDRunsAfterTheRelaunch(t *testing.T) {
	c := caseNamed(t, "copilot")
	id := "s_exitreuseo"
	var status atomic.Value
	status.Store("working")
	e, s := deadTurnEngine(t, c, id, &status, nil)
	recs := c.records(t)
	mid, _ := c.split(t, recs)
	feedRecords(s, c.mapper, recs[:mid])
	hubSays(e, s, &status, wire.StatusEnded)
	hubSays(e, s, &status, "idle")
	// The same calls again, as a fresh mapper names them.
	s.mu.Lock()
	s.mappers[c.mapper] = s.e.mapperFor(c.mapper, s.env)
	s.mu.Unlock()
	feedRecords(s, c.mapper, recs[:mid])
	if r := runningTurns(e, id); len(r) != 1 {
		t.Errorf("running turns %v, want the new one", r)
	}
	if sessWorking(s) != "true" {
		t.Error("the new turn does not count")
	}
}

// The fence stands aside while the chat shows another conversation: ids
// there are the other conversation's.
func TestFenceIsTheConversations(t *testing.T) {
	c := caseNamed(t, "copilot")
	var status atomic.Value
	status.Store("idle")
	_, s := deadTurnEngine(t, c, "s_exitconvco", &status, nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exit = &exitFence{At: time.Now(), Conv: "old", Items: map[string]bool{"t1": true}}
	s.conv = "new"
	it := harness.Item{ID: "t1", TurnID: "t1", Kind: harness.KindTurn, Turn: &harness.TurnBody{State: harness.StateRunning}}
	s.keepEndedLocked(&it, false)
	if it.Turn.State != harness.StateRunning {
		t.Error("another conversation's turn was ended")
	}
	s.conv = "old"
	s.keepEndedLocked(&it, false)
	if it.Turn.State != harness.StateInterrupted {
		t.Error("the dead conversation's turn runs again on a replay")
	}
}

// An end the agent wrote without a time of its own still ends the turn.
func TestUndatedTurnEndStands(t *testing.T) {
	c := caseNamed(t, "copilot")
	var status atomic.Value
	status.Store(wire.StatusEnded)
	_, s := deadTurnEngine(t, c, "s_exitundaco", &status, nil)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exit = &exitFence{At: time.Now(), Items: map[string]bool{"t1": true}}
	it := harness.Item{ID: "t1", TurnID: "t1", Kind: harness.KindTurn, Turn: &harness.TurnBody{State: harness.StateOK}}
	s.keepEndedLocked(&it, false)
	if it.Turn.State != harness.StateOK {
		t.Errorf("an undated end became %s", it.Turn.State)
	}
}

// The agent finished its turn and exited (/exit right after the reply);
// the reply's records reach the helper after the exit. The turn ends ok
// and gets one "finished" push.
func TestLateTurnEndGetsOneFinishedPush(t *testing.T) {
	c := caseNamed(t, "kimi")
	var status atomic.Value
	status.Store("working")
	var dones atomic.Int32
	e, s := deadTurnEngine(t, c, "s_exitlatepu", &status, func(string, string, string) { dones.Add(1) })
	recs := c.records(t)
	mid, rest := c.split(t, recs)
	feedRecords(s, c.mapper, recs[:mid])
	hubSays(e, s, &status, wire.StatusEnded)
	feedRecords(s, c.mapper, recs[mid:rest])
	waitFor(t, func() bool { return dones.Load() >= 1 })
	hubSays(e, s, &status, "idle")
	feedRecords(s, c.mapper, recs[rest:rest+1]) // the relaunched agent's prompt
	if n := dones.Load(); n != 1 {
		t.Errorf("%d finished pushes, want 1", n)
	}
}

// A kept fence too large to be one is not read.
func TestOversizedFenceIsDropped(t *testing.T) {
	togo := t.TempDir()
	var logged atomic.Int32
	e := New(Config{TogoHome: togo, Log: &Log{}, Logf: func(string, ...any) { logged.Add(1) }})
	id := "s_exitbigbig"
	path := filepath.Join(togo, "run", "harness", id+".exit")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	big := `{"at":"2026-11-20T00:00:00Z","items":{"` + strings.Repeat("x", maxFenceFile) + `":true}}`
	if err := os.WriteFile(path, []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	if f := e.loadExitFence(id); f != nil {
		t.Error("an oversized fence was read")
	}
	if logged.Load() == 0 {
		t.Error("nothing logged")
	}
}
