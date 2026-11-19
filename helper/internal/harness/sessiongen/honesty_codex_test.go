package sessiongen

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func codexShapes(t *testing.T) *CodexShapes {
	t.Helper()
	s, err := LoadCodexShapes(filepath.Join("..", "adapters", "codex", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Every message the Codex profile writes is one a recording holds, with
// recorded fields of recorded kinds and recorded states, over every turn
// shape it can write.
func TestHonestyCodex(t *testing.T) {
	shapes := codexShapes(t)
	home := t.TempDir()
	cwd := filepath.Join(home, "proj")
	p := &Codex{}
	l, err := Write(home, p, Script{Seed: 3, Start: time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC), Cwd: cwd, Turns: []Turn{
		{Say: 80, Tools: 2, Output: 300},
		{Say: 40, Agents: &Batch{Size: 3, Steps: 2, Background: true, Unreported: 1}},
		{Tools: 1, Interrupt: true},
		{Say: 10, Error: true},
		{Say: 20, New: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	p.Approval, p.Ask = true, true
	if err := l.Append(Turn{Say: 30, Tools: 1, Exit: true}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p.Journal())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	n := 0
	seen := map[string]bool{}
	for sc.Scan() {
		n++
		for _, d := range shapes.CheckLine(sc.Bytes()) {
			t.Errorf("line %d: %s", n, d)
		}
		var m CodexMsg
		_ = json.Unmarshal(sc.Bytes(), &m)
		k, _ := codexKind(m)
		seen[k] = true
	}
	for _, k := range []string{"thread/started", "turn/started", "turn/completed", "item/started", "item/completed",
		"item/agentMessage/delta", "item/reasoning/summaryTextDelta", "item/commandExecution/outputDelta",
		"turn/plan/updated", "turn/diff/updated", "thread/tokenUsage/updated", "account/rateLimits/updated",
		"thread/status/changed", "error", "request item/fileChange/requestApproval", "request item/tool/requestUserInput"} {
		if !seen[k] {
			t.Errorf("the profile wrote no %s", k)
		}
	}
	if l.Truth.Prompts != 6 || len(l.Truth.Agents) != 3 || !l.Truth.Exited {
		t.Errorf("truth %+v", l.Truth)
	}
	for _, m := range []CodexMsg{CodexResolved("th", 0), CodexSettings("th", cwd, CodexModel, "high", "never"),
		CodexLimitsUpdated(CodexRateLimits(5, 1, time.Now()))} {
		for _, d := range shapes.Check(m) {
			t.Errorf("%v: %s", m["method"], d)
		}
	}
}

// The check fails on a field, a kind, a state or a message no recording
// has: it can tell an invented message from a recorded one.
func TestHonestyCodexCatchesInventions(t *testing.T) {
	shapes := codexShapes(t)
	bad := []CodexMsg{
		{"method": "thread/compacted", "params": map[string]any{"threadId": "x"}},
		CodexTurnStarted("x", map[string]any{"id": "t", "status": "queued"}),
		CodexItemCompleted("x", "t", map[string]any{"type": "agentMessage", "id": "m", "text": 5}, time.Now()),
		CodexItemCompleted("x", "t", map[string]any{"type": "agentMessage", "id": "m", "mood": "calm"}, time.Now()),
	}
	for _, m := range bad {
		if len(shapes.Check(m)) == 0 {
			t.Errorf("an invented message passed: %v", m)
		}
	}
}
