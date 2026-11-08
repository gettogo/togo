package claude

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gettogo/togo/helper/internal/harness"
)

func trRec(line string) harness.Record {
	return harness.Record{Source: "transcript", At: time.Unix(0, 0), Data: json.RawMessage(line)}
}

// A local command (/model, typed by set_model) gets its output line and no
// reply or Stop hook. The turn it started ends there, so the session does
// not stay Working with every later prompt queued behind it.
func TestLocalCommandEndsItsTurn(t *testing.T) {
	h, tr := newHooks(env()), newTranscript(env())
	h.Map(hookAt("UserPromptSubmit", "2026-11-03T13:00:00.000Z"))
	r := mapAll(tr, []harness.Record{
		trRec(`{"type":"user","message":{"role":"user","content":"<command-name>/model</command-name>\n<command-message>model</command-message>\n<command-args>haiku</command-args>"},"uuid":"c1","timestamp":"2026-11-03T13:00:00.100Z","sessionId":"s2"}`),
		trRec(`{"type":"user","message":{"role":"user","content":"<local-command-stdout>Set model to Haiku</local-command-stdout>"},"uuid":"c2","timestamp":"2026-11-03T13:00:01.000Z","sessionId":"s2"}`),
	})
	if harness.MergeWorking(h.State(), tr.State()) {
		t.Fatalf("still working after a local command: hooks %v transcript %v", h.State(), tr.State())
	}
	turn := r.final["c1.turn"].Turn
	if turn == nil || turn.State != harness.StateOK || turn.Ended == nil {
		t.Fatalf("command turn = %+v", turn)
	}
	// A prompt after it works as usual.
	tr.Map(trRec(`{"type":"user","message":{"role":"user","content":"hi"},"uuid":"u2","timestamp":"2026-11-03T13:00:05.000Z","sessionId":"s2"}`))
	if !harness.MergeWorking(h.State(), tr.State()) {
		t.Fatal("a real prompt must work")
	}
}

// A slash command that goes to the model (a custom command, /init) gets a
// reply; its turn runs until that ends, as before.
func TestModelCommandKeepsItsTurn(t *testing.T) {
	tr := newTranscript(env())
	tr.Map(trRec(`{"type":"user","message":{"role":"user","content":"<command-name>/init</command-name>\n<command-message>init</command-message>"},"uuid":"c1","timestamp":"2026-11-03T13:00:00.100Z","sessionId":"s2"}`))
	if !harness.MergeWorking(tr.State()) {
		t.Fatal("a model command's turn must show working")
	}
}

// A subagent whose Agent call is not known still belongs to a turn.
func TestUnmatchedAgentGetsTheTurn(t *testing.T) {
	tr := newTranscript(env())
	tr.Map(trRec(`{"type":"user","message":{"role":"user","content":"go"},"uuid":"u1","timestamp":"2026-11-03T13:00:00.000Z","sessionId":"s2"}`))
	items, _ := tr.linkAgent("a6f7", "some prompt", time.Now())
	if len(items) != 1 || items[0].TurnID != "u1" {
		t.Fatalf("agent items %+v", items)
	}
}
