package e2e

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
	"github.com/gettogo/togo/helper/internal/harness/coverage"
	"github.com/gettogo/togo/helper/internal/harness/enginetest"
	"github.com/gettogo/togo/helper/internal/harness/sessiongen"
)

// A Claude Code session with two batches of background subagents (one of
// each batch never reported, so still at work), background shell jobs and
// a compaction. Claude Code's subagent files record no exit, so an agent
// ends only through its notification. The transcript is learned from the
// SessionStart hook; after a restart no hook comes again and the engine
// must find the same transcript and show the same chat.
func TestClaudeBackgroundAgentsAndRestart(t *testing.T) {
	coverage.Covers(t, "generated", "claude/capability/agents", "claude/capability/background",
		"claude/capability/compact", "claude/lifecycle/helper_restart")
	e := newEnv(t, 2*time.Hour)
	l, err := sessiongen.Write(e.home, &sessiongen.Claude{}, sessiongen.Script{Seed: 31, Start: e.start, Cwd: e.cwd,
		Turns: []sessiongen.Turn{
			{Say: 60, Tools: 2, Output: 300},
			{Say: 40, Jobs: 2, Agents: &sessiongen.Batch{Size: 3, Steps: 2, Background: true, Unreported: 1}},
			{Say: 40, Compact: true, Tools: 1, Output: 100},
			{Say: 40, Agents: &sessiongen.Batch{Size: 3, Steps: 1, Background: true, Unreported: 1}},
			{Say: 40},
		}})
	if err != nil {
		t.Fatal(err)
	}
	r := e.rig(t, "claude")
	hook, _ := json.Marshal(map[string]any{"hook_event_name": "SessionStart", "session_id": strings.TrimSuffix(lastPart(l.Main()), ".jsonl"),
		"transcript_path": l.Main(), "source": "startup"})
	r.Deliver("SessionStart", hook)
	ready := func(st enginetest.State) bool {
		return enginetest.Has(l.Truth.Last)(st) && countKind(st, harness.KindAgent) == 6
	}
	before := r.SettleUntil(15*time.Second, ready)
	check(t, "first read", before, l)
	if n := countKind(before, harness.KindBackground); n != 2 {
		t.Errorf("%d background job items, want 2", n)
	}
	running := 0
	for _, it := range before.Items {
		if it.Agent != nil && it.Agent.State == harness.StateRunning {
			running++
		}
	}
	if running != 2 {
		t.Errorf("%d agents running, want the 2 never reported", running)
	}
	if !hasNotice(before, harness.NoticeCompacted) {
		t.Error("no compaction notice")
	}

	r.Restart()
	after := r.SettleUntil(15*time.Second, ready)
	check(t, "after restart", after, l)
	fail(t, "restart", enginetest.Restarted(before, after), l)
}

func lastPart(p string) string { return p[strings.LastIndexByte(p, '/')+1:] }

func hasNotice(st enginetest.State, code string) bool {
	for _, it := range st.Items {
		if it.Notice != nil && it.Notice.Code == code {
			return true
		}
	}
	return false
}
